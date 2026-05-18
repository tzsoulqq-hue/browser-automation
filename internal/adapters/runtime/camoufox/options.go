package camoufox

import (
	"encoding/json"
	"strconv"
	"strings"

	browserautomationv1 "github.com/byte-v-forge/contracts-go/byte/v/forge/contracts/browserautomation/v1"
)

func serverOptions(cfg Config, session *browserautomationv1.BrowserSession) map[string]any {
	profile := session.GetProfile()
	labels := profile.GetLabels()
	options := map[string]any{
		"headless": cfg.Headless,
		"port":     cfg.ServerPort,
		"ws_path":  cfg.WSPathPrefix + safeID(session.GetSessionId()),
	}
	if profile.GetLocale() != "" {
		options["locale"] = profile.GetLocale()
	}
	if viewport := profile.GetViewport(); viewport != nil && viewport.GetWidth() > 0 && viewport.GetHeight() > 0 {
		options["window"] = []int32{viewport.GetWidth(), viewport.GetHeight()}
	}
	setStringOption(options, labels, "camoufox.os", "os")
	setBoolOption(options, labels, "camoufox.headless", "headless")
	setBoolOrStringOption(options, labels, "camoufox.geoip", "geoip")
	setBoolOption(options, labels, "camoufox.block_images", "block_images")
	setBoolOption(options, labels, "camoufox.block_webrtc", "block_webrtc")
	setBoolOption(options, labels, "camoufox.block_webgl", "block_webgl")
	setBoolOption(options, labels, "camoufox.disable_coop", "disable_coop")
	setBoolOption(options, labels, "camoufox.main_world_eval", "main_world_eval")
	setBoolOption(options, labels, "camoufox.enable_cache", "enable_cache")
	setBoolFloatOrStringOption(options, labels, "camoufox.humanize", "humanize")
	return options
}

func workerOptions(endpoint string, cfg Config, session *browserautomationv1.BrowserSession) map[string]any {
	profile := session.GetProfile()
	contextOptions := map[string]any{}
	if profile.GetLocale() != "" {
		contextOptions["locale"] = profile.GetLocale()
	}
	if profile.GetTimezone() != "" {
		contextOptions["timezone_id"] = profile.GetTimezone()
	}
	if profile.GetUserAgent() != "" {
		contextOptions["user_agent"] = profile.GetUserAgent()
	}
	if viewport := profile.GetViewport(); viewport != nil && viewport.GetWidth() > 0 && viewport.GetHeight() > 0 {
		contextOptions["viewport"] = map[string]int32{
			"width":  viewport.GetWidth(),
			"height": viewport.GetHeight(),
		}
		if viewport.GetDeviceScaleFactor() > 0 {
			contextOptions["device_scale_factor"] = viewport.GetDeviceScaleFactor()
		}
	}
	return map[string]any{
		"endpoint":        endpoint,
		"artifacts_dir":   cfg.ArtifactsDir,
		"context_options": contextOptions,
	}
}

func encodeOptions(options map[string]any) (string, error) {
	payload, err := json.Marshal(options)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func setStringOption(options map[string]any, labels map[string]string, labelKey, optionKey string) {
	value := strings.TrimSpace(labels[labelKey])
	if value == "" {
		return
	}
	if strings.Contains(value, ",") {
		parts := strings.Split(value, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				values = append(values, part)
			}
		}
		if len(values) > 0 {
			options[optionKey] = values
			return
		}
	}
	options[optionKey] = value
}

func setBoolOption(options map[string]any, labels map[string]string, labelKey, optionKey string) {
	value, ok := parseBoolLabel(labels[labelKey])
	if ok {
		options[optionKey] = value
	}
}

func setBoolOrStringOption(options map[string]any, labels map[string]string, labelKey, optionKey string) {
	raw := strings.TrimSpace(labels[labelKey])
	if raw == "" {
		return
	}
	if value, ok := parseBoolLabel(raw); ok {
		options[optionKey] = value
		return
	}
	options[optionKey] = raw
}

func setBoolFloatOrStringOption(options map[string]any, labels map[string]string, labelKey, optionKey string) {
	raw := strings.TrimSpace(labels[labelKey])
	if raw == "" {
		return
	}
	if value, ok := parseBoolLabel(raw); ok {
		options[optionKey] = value
		return
	}
	if value, err := strconv.ParseFloat(raw, 64); err == nil {
		options[optionKey] = value
		return
	}
	options[optionKey] = raw
}

func parseBoolLabel(raw string) (bool, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return false, false
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return value, true
}

func safeID(value string) string {
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-',
			r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}
