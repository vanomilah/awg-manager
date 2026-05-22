package managed

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSetASCParams_RejectsEmptyPayload(t *testing.T) {
	svc, _ := newCreateTestService(t)
	server, err := svc.Create(context.Background(), CreateServerRequest{
		Address:    "10.31.0.1",
		Mask:       "255.255.255.0",
		ListenPort: 52031,
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	if err := svc.SetASCParams(context.Background(), server.InterfaceName, json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected validation error for empty ASC payload")
	}
}

func TestSetASCParams_RejectsEmptyRequiredFields(t *testing.T) {
	svc, _ := newCreateTestService(t)
	server, err := svc.Create(context.Background(), CreateServerRequest{
		Address:    "10.32.0.1",
		Mask:       "255.255.255.0",
		ListenPort: 52032,
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	raw := json.RawMessage(`{
		"jc":3,"jmin":64,"jmax":256,"s1":15,"s2":16,
		"h1":"","h2":"1200000000","h3":"2400000000","h4":"3600000000"
	}`)
	err = svc.SetASCParams(context.Background(), server.InterfaceName, raw)
	if err == nil {
		t.Fatalf("expected validation error for empty required ASC text field")
	}
	if !strings.Contains(err.Error(), "h1") {
		t.Fatalf("expected error to mention missing h1, got: %v", err)
	}
}

func TestSetASCParams_AllowsEmptySignatureFields(t *testing.T) {
	svc, _ := newCreateTestService(t)
	server, err := svc.Create(context.Background(), CreateServerRequest{
		Address:    "10.33.0.1",
		Mask:       "255.255.255.0",
		ListenPort: 52033,
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	raw := json.RawMessage(`{
		"jc":3,"jmin":64,"jmax":256,"s1":15,"s2":16,
		"h1":"100000001","h2":"1200000002","h3":"2400000003","h4":"3600000004",
		"i1":"","i2":"","i3":"","i4":"","i5":""
	}`)
	if err := svc.SetASCParams(context.Background(), server.InterfaceName, raw); err != nil {
		t.Fatalf("SetASCParams must accept empty i1..i5: %v", err)
	}
}

func TestSetASCParams_ExtendedPairValidation(t *testing.T) {
	svc, _ := newCreateTestService(t)
	server, err := svc.Create(context.Background(), CreateServerRequest{
		Address:    "10.34.0.1",
		Mask:       "255.255.255.0",
		ListenPort: 52034,
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	base := `"jc":3,"jmin":64,"jmax":256,"s1":15,"s2":16,"h1":"100000001","h2":"1200000002","h3":"2400000003","h4":"3600000004"`

	if err := svc.SetASCParams(context.Background(), server.InterfaceName, json.RawMessage(`{`+base+`,"s3":null,"s4":10}`)); err == nil {
		t.Fatalf("expected error when s3 is null in extended payload")
	}
	if err := svc.SetASCParams(context.Background(), server.InterfaceName, json.RawMessage(`{`+base+`,"s3":8}`)); err == nil {
		t.Fatalf("expected error when s4 is missing in extended payload")
	}
	if err := svc.SetASCParams(context.Background(), server.InterfaceName, json.RawMessage(`{`+base+`,"s3":8,"s4":10}`)); err != nil {
		t.Fatalf("expected valid extended payload to pass: %v", err)
	}
}

func TestSetASCParams_AllowsZeroDisabledState(t *testing.T) {
	svc, _ := newCreateTestService(t)
	server, err := svc.Create(context.Background(), CreateServerRequest{
		Address:    "10.35.0.1",
		Mask:       "255.255.255.0",
		ListenPort: 52035,
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	raw := json.RawMessage(`{
		"jc":0,"jmin":0,"jmax":0,"s1":0,"s2":0,
		"h1":"","h2":"","h3":"","h4":""
	}`)
	if err := svc.SetASCParams(context.Background(), server.InterfaceName, raw); err != nil {
		t.Fatalf("zero/default disabled ASC payload must be accepted: %v", err)
	}
}

func TestSetASCParams_RejectsPartialDisabledState(t *testing.T) {
	svc, _ := newCreateTestService(t)
	server, err := svc.Create(context.Background(), CreateServerRequest{
		Address:    "10.37.0.1",
		Mask:       "255.255.255.0",
		ListenPort: 52037,
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	raw := json.RawMessage(`{
		"jc":0,"jmin":0,"jmax":0,"s1":0,"s2":0,
		"h1":"100","h2":"","h3":"","h4":""
	}`)
	if err := svc.SetASCParams(context.Background(), server.InterfaceName, raw); err == nil {
		t.Fatalf("expected validation error for partial disabled ASC state")
	}
}

func TestSetASCParams_RejectsJmaxNotGreaterThanJmin(t *testing.T) {
	svc, _ := newCreateTestService(t)
	server, err := svc.Create(context.Background(), CreateServerRequest{
		Address:    "10.36.0.1",
		Mask:       "255.255.255.0",
		ListenPort: 52036,
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	raw := json.RawMessage(`{
		"jc":3,"jmin":128,"jmax":128,"s1":15,"s2":16,
		"h1":"100000001","h2":"1200000002","h3":"2400000003","h4":"3600000004"
	}`)
	if err := svc.SetASCParams(context.Background(), server.InterfaceName, raw); err == nil {
		t.Fatalf("expected validation error when jmax <= jmin")
	}
}
