package mihomonative

import "testing"

func TestCompileVLESSXHTTPSelectsMihomo(t *testing.T) {
	raw := "vless://11111111-2222-3333-4444-555555555555@nl.example:443?type=xhttp&security=reality&sni=cdn.example&pbk=pub&sid=abcd&fp=chrome&path=%2Fapi&host=edge.example&mode=stream-up#NL-XHTTP"
	node, err := CompileVLESS(raw, EngineAuto, EngineSingbox)
	if err != nil {
		t.Fatal(err)
	}
	if node.SelectedEngine != EngineMihomo {
		t.Fatalf("engine=%q", node.SelectedEngine)
	}
	if node.Compatibility.Singbox.Supported {
		t.Fatal("xhttp unexpectedly supported by sing-box compiler")
	}
	if node.NativeConfig["network"] != "xhttp" {
		t.Fatalf("network=%v", node.NativeConfig["network"])
	}
	opts, ok := node.NativeConfig["xhttp-opts"].(map[string]interface{})
	if !ok || opts["mode"] != "stream-up" || opts["path"] != "/api" {
		t.Fatalf("xhttp opts=%#v", opts)
	}
}

func TestCompileVLESSForcedIncompatibleEngineFails(t *testing.T) {
	_, err := CompileVLESS("vless://id@host:443?type=xhttp#x", EngineSingbox, EngineMihomo)
	if err == nil {
		t.Fatal("expected compatibility error")
	}
}

func TestCompileVLESSUsesRoutingEngineForPortableTransport(t *testing.T) {
	node, err := CompileVLESS("vless://id@host:443?type=grpc&serviceName=svc#g", EngineAuto, EngineSingbox)
	if err != nil {
		t.Fatal(err)
	}
	if node.SelectedEngine != EngineSingbox {
		t.Fatalf("engine=%q", node.SelectedEngine)
	}
}
