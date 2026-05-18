package camoufox

import (
	"strings"
	"testing"

	browserautomationv1 "github.com/byte-v-forge/contracts-go/byte/v/forge/contracts/browserautomation/v1"
)

func TestServerOptionsFromProfile(t *testing.T) {
	cfg := Config{Headless: true, ServerPort: 0, WSPathPrefix: "test-"}
	session := &browserautomationv1.BrowserSession{
		SessionId: "session/1",
		Profile: &browserautomationv1.BrowserProfile{
			Locale: "en-US",
			Viewport: &browserautomationv1.BrowserViewport{
				Width:  1280,
				Height: 720,
			},
			Labels: map[string]string{
				"camoufox.os":              "windows,macos",
				"camoufox.headless":        "false",
				"camoufox.geoip":           "true",
				"camoufox.block_images":    "true",
				"camoufox.disable_coop":    "true",
				"camoufox.main_world_eval": "true",
				"camoufox.humanize":        "1.5",
			},
		},
	}

	options := serverOptions(cfg, session)

	if options["headless"] != false {
		t.Fatalf("headless = %#v, want false", options["headless"])
	}
	if options["ws_path"] != "test-session-1" {
		t.Fatalf("ws_path = %#v, want test-session-1", options["ws_path"])
	}
	if options["locale"] != "en-US" {
		t.Fatalf("locale = %#v, want en-US", options["locale"])
	}
	window := options["window"].([]int32)
	if window[0] != 1280 || window[1] != 720 {
		t.Fatalf("window = %#v, want 1280x720", window)
	}
	if options["geoip"] != true || options["block_images"] != true || options["disable_coop"] != true || options["main_world_eval"] != true {
		t.Fatalf("boolean options = %#v", options)
	}
	if options["humanize"] != 1.5 {
		t.Fatalf("humanize = %#v, want 1.5", options["humanize"])
	}
}

func TestWorkerOptionsFromProfile(t *testing.T) {
	cfg := Config{ArtifactsDir: "/tmp/artifacts"}
	session := &browserautomationv1.BrowserSession{
		Profile: &browserautomationv1.BrowserProfile{
			Locale:    "en-US",
			Timezone:  "Asia/Shanghai",
			UserAgent: "test-agent",
			Viewport: &browserautomationv1.BrowserViewport{
				Width:             390,
				Height:            844,
				DeviceScaleFactor: 3,
			},
		},
	}

	options := workerOptions("ws://127.0.0.1:1234/session", cfg, session)
	contextOptions := options["context_options"].(map[string]any)

	if contextOptions["locale"] != "en-US" || contextOptions["timezone_id"] != "Asia/Shanghai" || contextOptions["user_agent"] != "test-agent" {
		t.Fatalf("context options = %#v", contextOptions)
	}
	viewport := contextOptions["viewport"].(map[string]int32)
	if viewport["width"] != 390 || viewport["height"] != 844 {
		t.Fatalf("viewport = %#v, want 390x844", viewport)
	}
	if contextOptions["device_scale_factor"] != float64(3) {
		t.Fatalf("device_scale_factor = %#v, want 3", contextOptions["device_scale_factor"])
	}
}

func TestScanEndpoint(t *testing.T) {
	endpoints := make(chan string, 1)
	log := newTailBuffer(1024)

	scanEndpoint(log, strings.NewReader("ready\nWebsocket endpoint: ws://localhost:1234/hello\n"), endpoints)

	if got := <-endpoints; got != "ws://localhost:1234/hello" {
		t.Fatalf("endpoint = %q, want ws://localhost:1234/hello", got)
	}
	if !strings.Contains(log.String(), "Websocket endpoint") {
		t.Fatalf("log = %q, want endpoint line", log.String())
	}
}

func TestDecodeWorkerResponse(t *testing.T) {
	response, err := decodeWorkerResponse(`{
		"type":"task_result",
		"task_id":"task-1",
		"results":[{
			"command_id":"cmd-1",
			"command_key":"evaluate",
			"status":"BROWSER_COMMAND_STATUS_SUCCEEDED",
			"json_value":{"ok":true},
			"completed_at":"2026-05-18T12:00:00Z"
		}],
		"artifacts":[{
			"artifact_id":"artifact-1",
			"kind":"BROWSER_ARTIFACT_KIND_SCREENSHOT",
			"uri":"file:///tmp/artifact-1.png",
			"content_type":"image/png",
			"size_bytes":42,
			"created_at":"2026-05-18T12:00:00Z"
		}]
	}`)
	if err != nil {
		t.Fatalf("decodeWorkerResponse() error = %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].GetCommandId() != "cmd-1" {
		t.Fatalf("results = %#v", response.Results)
	}
	if !response.Results[0].GetJsonValue().GetStructValue().GetFields()["ok"].GetBoolValue() {
		t.Fatalf("json_value = %#v", response.Results[0].GetJsonValue())
	}
	if len(response.Artifacts) != 1 || response.Artifacts[0].GetArtifactId() != "artifact-1" {
		t.Fatalf("artifacts = %#v", response.Artifacts)
	}
}
