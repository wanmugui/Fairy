package local

import (
	"strings"
	"testing"
)

func TestBashPolicyBlocksDeniedCommands(t *testing.T) {
	p := DefaultBashPolicy()
	for _, cmd := range []string{
		"sudo apt install foo",
		"format c:",
		"shutdown -h now",
		"reboot",
	} {
		d := p.Check(cmd)
		if d.Allowed {
			t.Errorf("expected %q to be blocked, got allowed", cmd)
		}
		if !strings.Contains(strings.ToLower(d.Reason), "deny") {
			t.Errorf("expected deny reason for %q, got %q", cmd, d.Reason)
		}
	}
}

func TestBashPolicyBlocksDeniedPaths(t *testing.T) {
	p := DefaultBashPolicy()
	for _, cmd := range []string{
		"rm -rf C:\\Windows\\System32\\drivers\\etc",
		"cat /etc/passwd",
		"ls /boot/grub",
		"echo C:/Windows\\foo",
	} {
		d := p.Check(cmd)
		if d.Allowed {
			t.Errorf("expected %q to be blocked, got allowed", cmd)
		}
	}
}

func TestBashPolicyAllowsNormalCommands(t *testing.T) {
	p := DefaultBashPolicy()
	for _, cmd := range []string{
		"ls -la",
		"git status",
		"python -m pytest",
		"node server.js",
		"echo hello world",
		"FOO=bar python script.py", // env-assigned command
	} {
		d := p.Check(cmd)
		if !d.Allowed {
			t.Errorf("expected %q to be allowed, got blocked: %s", cmd, d.Reason)
		}
	}
}

func TestBashPolicyWhitelistEnforced(t *testing.T) {
	p := BashPolicy{AllowCommands: []string{"ls", "cat"}}
	for _, cmd := range []string{"ls -la", "cat foo.txt"} {
		d := p.Check(cmd)
		if !d.Allowed {
			t.Errorf("expected %q allowed under whitelist, got blocked: %s", cmd, d.Reason)
		}
	}
	for _, cmd := range []string{"rm foo", "echo hi"} {
		d := p.Check(cmd)
		if d.Allowed {
			t.Errorf("expected %q blocked under whitelist, got allowed", cmd)
		}
	}
}

func TestBashPolicyEmptyCommandIsBlocked(t *testing.T) {
	p := DefaultBashPolicy()
	if d := p.Check("   "); d.Allowed {
		t.Fatalf("expected empty command to be blocked")
	}
}

func TestBashPolicyDenyTakesPriorityOverAllow(t *testing.T) {
	p := BashPolicy{
		AllowCommands: []string{"sudo"},
		DenyCommands:  []string{"sudo"},
	}
	d := p.Check("sudo apt install foo")
	if d.Allowed {
		t.Fatalf("deny should beat allow")
	}
}
