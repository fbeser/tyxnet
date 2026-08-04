package main

import "testing"

func TestRunWebModeFlags(t *testing.T) {
	_, mode, err := runFlags(nil)
	if err != nil || mode != "config" {
		t.Fatalf("default mode: %q %v", mode, err)
	}
	_, mode, err = runFlags([]string{"--local-web"})
	if err != nil || mode != "local" {
		t.Fatalf("local mode: %q %v", mode, err)
	}
	if _, _, err = runFlags([]string{"--local-web", "--lan-web"}); err == nil {
		t.Fatal("expected conflicting web mode flags to fail")
	}
}
