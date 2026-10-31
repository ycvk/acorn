package wake

import (
	"slices"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestBriefingPartsFollowTheHeadingsAndDropEmptyOnes(t *testing.T) {
	body := "周日，08:00。\n\n## 今天的安排\n\n- 没有到期的约定\n- **10:00** 开会\n\n## 追踪\n\n"
	want := []BriefingPart{{Body: "周日，08:00。"}, {Heading: "今天的安排", Body: "- 没有到期的约定\n- **10:00** 开会"}}
	if got := BriefingParts(body, nil); !slices.Equal(got, want) {
		t.Fatalf("parts = %#v", got)
	}
}

func TestBriefingPartsLeaveOutTheNewsAndTheFocusOncePicked(t *testing.T) {
	body := "## 国际\n1. 停火谈判\n## 今日关注\n- 第 1 条：停火\n## 今天\n10:00 开会\n"
	if got := BriefingParts(body, []core.BriefingStory{{StoryID: 1, Section: "国际"}}); !slices.Equal(got, []BriefingPart{{Heading: "今天", Body: "10:00 开会"}}) {
		t.Fatalf("with picked stories: %#v", got)
	}
	if got := BriefingParts(body, nil); len(got) != 3 {
		t.Fatalf("without picked stories every part stays: %#v", got)
	}
}
