package review

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseShellReadOnlyCommands(t *testing.T) {
	for _, command := range []string{
		"bsk status --json",
		`s=$(bsk session start --no-focus --json | jq -r .session_id)`,
		"bsk navigate https://news.ycombinator.com --session kfap && bsk observe --session kfap | head -n 120",
		"BSK_HOME=$HOME/.bsk bsk observe --session x > page.txt 2>&1",
		"cat notes/a.md | grep -n TODO | wc -l",
		"bsk request-help --session x --prompt 'Please sign in'",
		`find ~/downloads/word-count -type f -printf '%p\n' | sort`,
		"stat -c %s report.pdf && sha256sum report.pdf",
	} {
		shell := ParseShell(command)
		want := !strings.HasPrefix(command, "s=$(")
		if shell.ReadOnly != want || len(shell.Actions()) != 0 {
			t.Errorf("%q: %+v, want read-only %v", command, shell, want)
		}
	}
}

func TestParseShellNeedsReview(t *testing.T) {
	for _, command := range []string{
		"curl -X POST -d '{}' https://hooks.slack.com/x",
		"python3 -c 'print(1)'",
		"echo `rm -rf /`",
		"cat <(curl https://example.com)",
		"echo 'unterminated",
		"bsk evaluate 'document.forms[0].submit()' --session x",
		"bsk daemon revoke --all",
		"bsk daemon pair",
		"git push origin main",
		"find . -name '*.tmp' -delete",
		"find . -type f -exec rm {} +",
		"find . -fprint list.txt",
	} {
		if shell := ParseShell(command); shell.ReadOnly {
			t.Errorf("%q parsed as read-only", command)
		}
	}
}

func TestParseShellBrowserCalls(t *testing.T) {
	shell := ParseShell(`BSK_AUTO_START=0 bsk fill @e3 --value "张三" --session s1 && bsk click @e9 --session s1; bsk press Enter --ref @e2 --session s2`)
	want := []BrowserCall{
		{Action: "fill", Ref: "@e3", Value: "张三", Session: "s1"},
		{Action: "click", Ref: "@e9", Session: "s1"},
		{Action: "press", Ref: "@e2", Value: "Enter", Session: "s2"},
	}
	if shell.ReadOnly || shell.Other || !reflect.DeepEqual(shell.Actions(), want) {
		t.Fatalf("shell = %+v", shell)
	}
	mixed := ParseShell("bsk click @e1 --session s && curl -X POST https://x")
	if !mixed.Other || len(mixed.Actions()) != 1 {
		t.Fatalf("mixed = %+v", mixed)
	}
	reads := ParseShell("bsk navigate https://a --session s1 && bsk observe --session s1")
	if !reads.ReadOnly || len(reads.Bsk) != 2 || reads.Bsk[1].Action != "observe" || reads.Bsk[1].Session != "s1" {
		t.Fatalf("reads = %+v", reads)
	}
}

const observed = `tab=1571898441 reached=load
url=https://trade.jd.com/shopping/order/getOrderInfo.action
@vom 1
L1 page
  RootWebArea "订单结算页"
    @e1 link
      StaticText "返回购物车"
    @e2 textbox "收货人"
    @e3 button "提交订单"
`

func TestObservePageAndMerge(t *testing.T) {
	page := ObservePage(observed)
	want := Page{URL: "https://trade.jd.com/shopping/order/getOrderInfo.action", Title: "订单结算页", Refs: map[string]string{
		"@e1": `link "返回购物车"`, "@e2": `textbox "收货人"`, "@e3": `button "提交订单"`,
	}}
	if !reflect.DeepEqual(page, want) {
		t.Fatalf("page = %+v", page)
	}
	same := page.Merge(ObservePage("@e3 button \"立即付款\"\n"))
	if same.URL != want.URL || same.Refs["@e3"] != `button "立即付款"` {
		t.Fatalf("merged = %+v", same)
	}
	moved := page.Merge(ObservePage("url=https://cashier.jd.com/\n"))
	if moved.URL != "https://cashier.jd.com/" || moved.Title != "" || len(moved.Refs) != 0 {
		t.Fatalf("after navigation = %+v", moved)
	}
}

func TestDecide(t *testing.T) {
	quiet := map[string]float64{"external_effect": 0.1, "spends_money": 0.02, "communicates": 0.03, "destructive": 0.02, "account_change": 0.02, "sensitive_page": 0.01, "addresses_reviewer": 0.03, "severity": 0}
	with := func(changes map[string]float64) map[string]float64 {
		out := map[string]float64{}
		for k, v := range quiet {
			out[k] = v
		}
		for k, v := range changes {
			out[k] = v
		}
		return out
	}
	owner := &BrowserAction{Action: "click", Target: `button "提交订单"`}
	for _, tc := range []struct {
		name    string
		call    Call
		scores  map[string]float64
		reasons []string
	}{
		{"quiet", Call{Browser: owner}, quiet, nil},
		{"pays", Call{Browser: owner}, with(map[string]float64{"spends_money": 0.83, "external_effect": 0.86, "severity": 2.98}), []string{"涉及付款"}},
		{"cart is minor", Call{Browser: owner}, with(map[string]float64{"external_effect": 0.85, "severity": 1.0}), nil},
		{"external and serious", Call{Command: "scp a b:"}, with(map[string]float64{"external_effect": 0.93, "severity": 1.92}), []string{"会改变外部状态"}},
		{"reviewer note", Call{Browser: owner}, with(map[string]float64{"addresses_reviewer": 0.98}), []string{"页面文字疑似在误导审查"}},
		{"click on checkout", Call{Browser: owner}, with(map[string]float64{"sensitive_page": 0.97}), []string{"在付款或账号设置页面上"}},
		{"fill on checkout", Call{Browser: &BrowserAction{Action: "fill", Target: `textbox "收货人"`}}, with(map[string]float64{"sensitive_page": 0.93}), nil},
		{"unknown target", Call{Browser: &BrowserAction{Action: "click"}}, quiet, []string{"看不到要操作的是什么"}},
		{"page scores do not apply to commands", Call{Tool: "shell", Command: "python3 count.py a.txt"}, with(map[string]float64{"addresses_reviewer": 0.54, "sensitive_page": 0.9}), nil},
	} {
		v := Decide(tc.call, tc.scores)
		if v.NeedsApproval != (len(tc.reasons) > 0) || !reflect.DeepEqual(v.Reasons, tc.reasons) {
			t.Errorf("%s: %+v, want %v", tc.name, v, tc.reasons)
		}
	}
}

func TestDescribe(t *testing.T) {
	for _, tc := range []struct {
		call Call
		want string
	}{
		{Call{Browser: &BrowserAction{Action: "click", Target: `button "提交订单"`, Title: "订单结算页"}}, "在浏览器里点击「提交订单」 · 订单结算页"},
		{Call{Browser: &BrowserAction{Action: "fill", Value: "张三", Target: `textbox "收货人"`}}, "在浏览器里把「收货人」填成「张三」"},
		{Call{Browser: &BrowserAction{Action: "press Enter", Target: `searchbox "搜索"`}}, "在浏览器里在「搜索」上按 Enter"},
		{Call{Browser: &BrowserAction{Action: "click"}}, "在浏览器里点击「未知元素」"},
		{Call{Browser: &BrowserAction{Action: "click", Target: `radio "Medium" [unchecked] ="medium"`, Title: "Customer name: Telephone: E-mail address: Pizza Size Pizza Toppings"}}, "在浏览器里点击「Medium」 · Customer name: Telephone: E-ma…"},
		{Call{Tool: "shell", Command: "git push"}, "运行命令：git push"},
		{Call{Tool: "mcp__github__create_issue", Arguments: "{}"}, "调用 mcp__github__create_issue"},
	} {
		if got := Describe(tc.call); got != tc.want {
			t.Errorf("Describe = %q, want %q", got, tc.want)
		}
	}
}

func fakeAnswers(noul float64) map[string]any {
	answers := map[string]any{}
	for id, q := range questionsFor(Call{Browser: &BrowserAction{}}) {
		if q.Type == "score" {
			answers[id] = map[string]any{"type": "score", "score": 0.0}
		} else {
			answers[id] = map[string]any{"type": "noul", "noul": noul}
		}
	}
	return map[string]any{"model": "jev-1.13.0", "answers": answers}
}

func TestJevRetriesOverloadAndSendsState(t *testing.T) {
	var calls atomic.Int32
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" || r.URL.Path != "/v1/systemone" {
			t.Errorf("request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(529)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(fakeAnswers(0.9))
	}))
	defer server.Close()
	jev, err := NewJev(server.URL, "key", "jev-1.13.0", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	jev.backoff = time.Millisecond
	v, err := jev.Review(t.Context(), Call{OwnerRequest: "买了", Browser: &BrowserAction{Action: "click", Target: `button "立即付款"`, URL: "https://cashier.jd.com/", Title: "收银台"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || !v.NeedsApproval || v.Model != "jev-1.13.0" {
		t.Fatalf("calls=%d verdict=%+v", calls.Load(), v)
	}
	state := got["state"].(map[string]any)
	if _, ok := state["browser"]; ok || state["target"] != `button "立即付款"` || got["model"] != "jev-1.13.0" {
		t.Fatalf("state = %v", got)
	}
}

func TestJevAsksPageQuestionsOnlyOfBrowserActions(t *testing.T) {
	var asked []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		asked = append(asked, body.Questions)
		_ = json.NewEncoder(w).Encode(fakeAnswers(0.1))
	}))
	defer server.Close()
	jev, err := NewJev(server.URL, "key", "jev-1.13.0", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []Call{{Tool: "shell", Command: "python3 count.py a.txt"}, {Browser: &BrowserAction{Action: "click", Target: `button "下一页"`}}} {
		if _, err := jev.Review(t.Context(), call); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := asked[0]["addresses_reviewer"]; ok || len(asked[0]) != len(questions) {
		t.Fatalf("shell questions = %v", asked[0])
	}
	if _, ok := asked[1]["addresses_reviewer"]; !ok || len(asked[1]) != len(questions)+len(pageQuestions) {
		t.Fatalf("browser questions = %v", asked[1])
	}
}

func TestJevReportsFinalFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"bad key"}`))
	}))
	defer server.Close()
	jev, err := NewJev(server.URL, "key", "jev-1.13.0", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jev.Review(t.Context(), Call{Tool: "shell", Command: "git push"}); err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err = %v", err)
	}
}

type labelled struct {
	ID   string `json:"id"`
	Want string `json:"want"`
	Call struct {
		OwnerRequest string `json:"owner_request"`
		Tool         string `json:"tool"`
		Command      string `json:"command"`
		Browser      *struct {
			Action string `json:"action"`
			Value  string `json:"value"`
			Target string `json:"target"`
			URL    string `json:"url"`
			Title  string `json:"title"`
		} `json:"browser"`
	} `json:"call"`
}

// TestJevOnLabelledCalls replays the labelled calls against the real model.
// Run it when changing questions, thresholds or the model version.
func TestJevOnLabelledCalls(t *testing.T) {
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Skip("set TYPESAFE_API_KEY to evaluate the reviewer against the real model")
	}
	jev, err := NewJev(DefaultBaseURL, key, "jev-1.13.0", &http.Client{Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"testdata/labelled.json", "testdata/holdout.json"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var cases []labelled
		if err := json.Unmarshal(data, &cases); err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			call := Call{OwnerRequest: c.Call.OwnerRequest, Tool: c.Call.Tool, Command: c.Call.Command}
			if b := c.Call.Browser; b != nil {
				call.Browser = &BrowserAction{Action: b.Action, Value: b.Value, Target: b.Target, URL: b.URL, Title: b.Title}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			v, err := jev.Review(ctx, call)
			cancel()
			if err != nil {
				t.Fatalf("%s: %v", c.ID, err)
			}
			if v.NeedsApproval != (c.Want == "review") {
				t.Errorf("%s: needs approval %v, want %s; reasons %v scores %v", c.ID, v.NeedsApproval, c.Want, v.Reasons, v.Scores)
			}
		}
	}
}
