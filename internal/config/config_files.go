package config

import "fmt"

// maxFileMiB bounds both file caps.
const maxFileMiB = 1024

// FilesConfig caps the files of conversations: one the owner sends the agent
// from the app, and one the agent sends the owner from its workstation.
type FilesConfig struct {
	MaxUploadMiB int `yaml:"max_upload_mib"`
	MaxSendMiB   int `yaml:"max_send_mib"`
}

// MaxUploadBytes is the largest file the owner can send.
func (f FilesConfig) MaxUploadBytes() int64 { return int64(f.MaxUploadMiB) << 20 }

// MaxSendBytes is the largest file the agent can send.
func (f FilesConfig) MaxSendBytes() int64 { return int64(f.MaxSendMiB) << 20 }

func (c *Config) validateFiles() error {
	if c.Files.MaxUploadMiB < 1 || c.Files.MaxUploadMiB > maxFileMiB {
		return fmt.Errorf("files.max_upload_mib must be between 1 and %d", maxFileMiB)
	}
	if c.Files.MaxSendMiB < 1 || c.Files.MaxSendMiB > maxFileMiB {
		return fmt.Errorf("files.max_send_mib must be between 1 and %d", maxFileMiB)
	}
	return nil
}
