package main

import "testing"

func TestGlobalCheckinModelDefault(t *testing.T) {
	c, err := ParseConfig([]byte(`{}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := c.Global.CheckinModel; got != "global:hy3" {
		t.Fatalf("global.checkin_model=%q want global:hy3", got)
	}
}

func TestGlobalCheckinModelOverride(t *testing.T) {
	c, err := ParseConfig([]byte(`{"global":{"checkin_model":"global:hy4-preview-f"}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := c.Global.CheckinModel; got != "global:hy4-preview-f" {
		t.Fatalf("global.checkin_model=%q", got)
	}
}

func TestGlobalCheckinModelBlankFallsBack(t *testing.T) {
	c, err := ParseConfig([]byte(`{"global":{"checkin_model":"   "}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := c.Global.CheckinModel; got != "global:hy3" {
		t.Fatalf("blank global.checkin_model=%q want default", got)
	}
}
