package report

import "testing"

func TestExitCode(t *testing.T) {
	findings := []Finding{
		{Severity: SeverityOK},
		{Severity: SeverityWarn},
	}
	if code, err := ExitCode(findings, ""); err != nil || code != 0 {
		t.Fatalf("ExitCode default = %d, %v", code, err)
	}
	if code, err := ExitCode(findings, "fail"); err != nil || code != 0 {
		t.Fatalf("ExitCode fail = %d, %v", code, err)
	}
	if code, err := ExitCode(findings, "warn"); err != nil || code != 1 {
		t.Fatalf("ExitCode warn = %d, %v", code, err)
	}
}

func TestSummarize(t *testing.T) {
	s := Summarize([]Finding{{Severity: SeverityInfo}, {Severity: SeverityOK}, {Severity: SeverityWarn}, {Severity: SeverityFail}})
	if s.Info != 1 || s.OK != 1 || s.Warn != 1 || s.Fail != 1 || s.Total != 4 {
		t.Fatalf("Summarize() = %+v", s)
	}
}
