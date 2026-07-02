package job

import "testing"

const sampleFreestyleXML = `<?xml version='1.1' encoding='UTF-8'?><project>` +
	`<description>old desc</description>` +
	`<canRoam>true</canRoam>` +
	`<disabled>false</disabled>` +
	`<triggers><hudson.triggers.TimerTrigger><spec>H * * * *</spec></hudson.triggers.TimerTrigger></triggers>` +
	`<concurrentBuild>false</concurrentBuild>` +
	`<properties/>` +
	`<builders><hudson.tasks.Shell><command>echo old</command></hudson.tasks.Shell></builders>` +
	`<publishers><hudson.plugins.emailext.ExtendedEmailPublisher plugin="email-ext">` +
	`<recipientList>old@example.com</recipientList>` +
	`<configuredTriggers><hudson.plugins.emailext.plugins.trigger.FailureTrigger></hudson.plugins.emailext.plugins.trigger.FailureTrigger></configuredTriggers>` +
	`<presendScript>$DEFAULT_PRESEND_SCRIPT</presendScript>` +
	`</hudson.plugins.emailext.ExtendedEmailPublisher></publishers>` +
	`<buildWrappers/></project>`

// TestMergeFreestyleConfig_OnlyTouchesChangedFields verifies that updating
// only --description leaves shell/schedule/email untouched — the exact bug
// this file fixes (Go previously rebuilt the whole config.xml from flags).
func TestMergeFreestyleConfig_OnlyTouchesChangedFields(t *testing.T) {
	newDesc := "new description"
	f := FreestyleUpdateFields{Description: &newDesc}

	out, err := MergeFreestyleConfig(sampleFreestyleXML, f)
	if err != nil {
		t.Fatalf("MergeFreestyleConfig: %v", err)
	}

	if got := extractTag(out, "description"); got != newDesc {
		t.Errorf("description = %q, want %q", got, newDesc)
	}
	if got := extractShellCommand(out); got != "echo old" {
		t.Errorf("shell command changed: got %q, want unchanged %q", got, "echo old")
	}
	if got := extractSchedule(out); got != "H * * * *" {
		t.Errorf("schedule changed: got %q, want unchanged %q", got, "H * * * *")
	}
	if got := extractEmailRecipients(out); got != "old@example.com" {
		t.Errorf("email recipients changed: got %q, want unchanged %q", got, "old@example.com")
	}
}

func TestMergeFreestyleConfig_ShellUpdate(t *testing.T) {
	newShell := "echo new"
	f := FreestyleUpdateFields{Shell: &newShell}

	out, err := MergeFreestyleConfig(sampleFreestyleXML, f)
	if err != nil {
		t.Fatalf("MergeFreestyleConfig: %v", err)
	}
	if got := extractShellCommand(out); got != newShell {
		t.Errorf("shell command = %q, want %q", got, newShell)
	}
	if got := extractTag(out, "description"); got != "old desc" {
		t.Errorf("description changed unexpectedly: got %q", got)
	}
}

func TestValidateEmailFilterFlags_RequiresEmail(t *testing.T) {
	err := validateEmailFilterFlags("", false, []string{"fail"}, "", "", false)
	if err == nil {
		t.Fatal("expected error when keywords set without email")
	}
}

func TestValidateEmailFilterFlags_InvalidRegex(t *testing.T) {
	err := validateEmailFilterFlags("a@b.com", true, nil, "(unterminated", "", false)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestValidateEmailFilterFlags_OK(t *testing.T) {
	err := validateEmailFilterFlags("a@b.com", true, []string{"fail"}, "err.*", "always", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
