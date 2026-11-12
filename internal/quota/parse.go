// Package quota turns a provider usage payload into a Reading.
//
// The field names match the endpoints the quota widget already polls:
//
//	Claude      GET https://api.anthropic.com/api/oauth/usage
//	Codex       GET https://chatgpt.com/backend-api/wham/usage
//	Grok        GET https://cli-chat-proxy.grok.com/v1/billing
//	Antigravity agy --print /usage --output-format json
//
// Parsing is pure. HTTP and the agy process live in fetch.go so tests
// never open a socket.
package quota

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/maci0/subman/internal/model"
	"golang.org/x/text/cases"
)

// fold matches plan text case-insensitively without a locale. strings.ToLower
// follows the default Unicode case map, which leaves "İ" (U+0130) as itself,
// so a plan spelled that way would miss the ASCII tier it names.
func fold(s string) string { return cases.Fold().String(s) }

const maxWindows = 12

// Parse dispatches on provider. A body that names no meter returns a
// reading with ok=false and error "bad-body": an empty meter list would
// draw as a healthy zero.
func Parse(p model.Provider, body []byte, now time.Time) model.Reading {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return fail("bad-body", now)
	}
	obj, _ := raw.(map[string]any)
	if obj == nil {
		return fail("bad-body", now)
	}
	var r model.Reading
	switch p {
	case model.ProviderClaude:
		r = parseClaude(obj, now)
	case model.ProviderCodex:
		r = parseCodex(obj, now)
	case model.ProviderGrok:
		r = parseGrok(obj, now)
	case model.ProviderAntigravity:
		r = parseAntigravity(obj, now)
	default:
		return fail("unknown-provider", now)
	}
	r.PolledAt = now.UTC()
	if r.OK && len(r.Windows) == 0 && r.Credits.ResetCount == nil && r.Credits.Used == nil && r.Credits.Balance == nil {
		return fail("bad-body", now)
	}
	return r
}

func fail(err string, now time.Time) model.Reading {
	return model.Reading{OK: false, Error: err, PolledAt: now.UTC(), Windows: []model.Window{}}
}

func parseClaude(obj map[string]any, now time.Time) model.Reading {
	r := model.Reading{OK: true, Windows: []model.Window{}}
	if w, ok := claudeSession(obj); ok {
		r.Windows = append(r.Windows, w)
	}
	r.Windows = append(r.Windows, claudeWeekly(obj)...)
	extra := asMap(obj["extra_usage"])
	r.Credits.Enabled = asBool(extra["is_enabled"])
	r.Credits.Used = finite(extra["used_credits"])
	r.Credits.Limit = finite(extra["monthly_limit"])
	r.Credits.Currency = asString(extra["currency"])
	r.Plan = claudePlan(asString(obj["subscription_type"]), asString(obj["rate_limit_tier"]))
	if r.Plan == "" {
		r.Plan = claudePlan("", "")
	}
	r.TierID = matchTier(model.ProviderClaude, r.Plan)
	_ = now
	return r
}

// claudePlan maps the credential's subscription and tier strings onto the
// names the website shows. max_20x is Ultra.
func claudePlan(subscription, tier string) string {
	s := fold(subscription + " " + tier)
	if strings.Contains(s, "ultra") || strings.Contains(s, "20x") || strings.Contains(s, "max_20") {
		return "Ultra"
	}
	if strings.Contains(s, "max") {
		return "Max"
	}
	if strings.Contains(s, "pro") {
		return "Pro"
	}
	if strings.Contains(s, "team") {
		return "Team"
	}
	return ""
}

func claudeSession(obj map[string]any) (model.Window, bool) {
	if limits, ok := obj["limits"].([]any); ok {
		for _, item := range limits {
			m := asMap(item)
			if m["kind"] != "session" && m["group"] != "session" {
				continue
			}
			if m["is_active"] == false {
				return model.Window{}, false
			}
			return model.Window{Label: "5-hour", Util: finite(m["percent"]), ResetsAt: isoTime(m["resets_at"])}, true
		}
	}
	five := asMap(obj["five_hour"])
	if len(five) == 0 {
		return model.Window{}, false
	}
	return model.Window{Label: "5-hour", Util: finite(first(five, "utilization", "percent")), ResetsAt: isoTime(five["resets_at"])}, true
}

func claudeWeekly(obj map[string]any) []model.Window {
	var out []model.Window
	if limits, ok := obj["limits"].([]any); ok && len(limits) > 0 {
		for _, item := range limits {
			m := asMap(item)
			if m["kind"] == "session" || m["group"] == "session" {
				continue
			}
			label := "All models"
			scope := asMap(m["scope"])
			if s := asString(scope["surface"]); s != "" {
				label = trimLabel(s)
			} else if name := asString(asMap(scope["model"])["display_name"]); name != "" {
				label = trimLabel(name)
			}
			if m["kind"] == "weekly_all" {
				label = "All models"
			}
			out = append(out, model.Window{Label: label, Util: finite(m["percent"]), ResetsAt: isoTime(m["resets_at"])})
			if len(out) >= maxWindows {
				break
			}
		}
		return out
	}
	// The older shape has one block per weekly limit, keyed seven_day or
	// seven_day_<what>. Every such block is read, not a fixed list, so a
	// limit Anthropic adds shows up without a change here. "seven_day" sorts
	// first, the rest by name.
	keys := make([]string, 0, len(obj))
	for key := range obj {
		if key == "seven_day" || strings.HasPrefix(key, "seven_day_") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		block := asMap(obj[key])
		if len(block) == 0 || len(out) >= maxWindows {
			continue
		}
		label := "All models"
		if what := strings.TrimPrefix(key, "seven_day_"); what != key {
			label = titleWords(trimLabel(strings.ReplaceAll(what, "_", " ")))
		}
		out = append(out, model.Window{
			Label:    label,
			Util:     finite(block["utilization"]),
			ResetsAt: isoTime(block["resets_at"]),
		})
	}
	return out
}

func parseCodex(obj map[string]any, now time.Time) model.Reading {
	r := model.Reading{OK: true, Windows: []model.Window{}}
	plan := titleWords(trimLabel(strings.ReplaceAll(asString(obj["plan_type"]), "_", " ")))
	if plan != "" {
		r.Plan = plan
	}
	r.TierID = matchTier(model.ProviderCodex, r.Plan)
	// Every limit the payload carries has its own windows and reset times:
	// the main one, code review, and any additional per-feature limit. The
	// shapes are the Codex CLI's (codex-backend-openapi-models,
	// RateLimitStatusPayload and AdditionalRateLimitDetails).
	windows := func(prefix string, details map[string]any) {
		for _, key := range []string{"primary_window", "secondary_window"} {
			w, ok := codexWindow(asMap(details[key]), key, now)
			if !ok || len(r.Windows) >= maxWindows {
				continue
			}
			if prefix != "" {
				w.Label = prefix + ", " + fold(w.Label)
			}
			r.Windows = append(r.Windows, w)
		}
	}
	windows("", asMap(obj["rate_limit"]))
	windows("Code review", asMap(obj["code_review_rate_limit"]))
	if extra, ok := obj["additional_rate_limits"].([]any); ok {
		for _, item := range extra {
			limit := asMap(item)
			name := asString(first(limit, "limit_name", "metered_feature"))
			if name == "" {
				name = "Additional limit"
			}
			windows(titleWords(trimLabel(strings.ReplaceAll(name, "_", " "))), asMap(limit["rate_limit"]))
		}
	}
	// A model that is limited on its own states when it comes back. It has
	// no percent, so the meter is absent and only the reset time shows.
	models := asMap(obj["model_usage"])
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		usage := asMap(models[name])
		if back := isoTime(usage["available_at"]); back != nil && usage["available"] == false && len(r.Windows) < maxWindows {
			r.Windows = append(r.Windows, model.Window{Label: trimLabel(name), ResetsAt: back})
		}
	}
	credits := asMap(obj["credits"])
	r.Credits.Enabled = asBool(credits["has_credits"]) || credits["balance"] != nil
	r.Credits.Balance = finite(credits["balance"])
	r.Credits.Unlimited = asBool(credits["unlimited"])
	resets := asMap(obj["rate_limit_reset_credits"])
	if _, present := obj["rate_limit_reset_credits"]; present {
		n := 0
		if v := finite(resets["available_count"]); v != nil {
			n = int(*v)
		}
		r.Credits.ResetCount = &n
		if v := finite(resets["applicable_available_count"]); v != nil {
			a := int(*v)
			r.Credits.ResetApplies = &a
		}
	}
	return r
}

func codexWindow(block map[string]any, name string, now time.Time) (model.Window, bool) {
	util := finite(block["used_percent"])
	if util == nil {
		return model.Window{}, false
	}
	clamped := math.Min(100, math.Max(0, *util))
	var windowS int64
	if v := finite(block["limit_window_seconds"]); v != nil && *v > 0 && *v <= math.MaxInt64 {
		windowS = int64(*v)
	}
	label := codexLabel(windowS, name)
	var resets *time.Time
	if v := finite(block["reset_at"]); v != nil {
		resets = unixMaybe(*v)
	} else if t := afterSeconds(now, finite(block["reset_after_seconds"])); t != nil {
		resets = t
	}
	return model.Window{Label: label, Util: &clamped, ResetsAt: resets}, true
}

func codexLabel(windowS int64, name string) string {
	const (
		hour = 3600
		day  = 86400
	)
	switch {
	case windowS == 0:
		return trimLabel(strings.ReplaceAll(name, "_", " "))
	case windowS <= 6*hour:
		return "Current session"
	case windowS <= 2*day:
		return strconv.FormatInt((windowS+hour/2)/hour, 10) + "-hour"
	case windowS >= 6*day && windowS <= 8*day:
		return "Weekly"
	case windowS >= 27*day && windowS <= 32*day:
		return "Monthly"
	default:
		return strconv.FormatInt((windowS+day/2)/day, 10) + "-day"
	}
}

func parseGrok(obj map[string]any, now time.Time) model.Reading {
	r := model.Reading{OK: true, Plan: "Grok", Windows: []model.Window{}}
	cfg := asMap(obj["config"])
	if len(cfg) == 0 {
		cfg = obj
	}
	if tier := asString(first(cfg, "subscription_tier_display", "tier", "plan")); tier != "" {
		r.Plan = trimLabel(tier)
		r.TierID = matchTier(model.ProviderGrok, r.Plan)
	}
	if w, ok := grokPeriod(cfg, now); ok {
		r.Windows = append(r.Windows, w)
	}
	for _, key := range []string{"weekly", "monthly", "credits"} {
		if w, ok := grokPeriod(asMap(obj[key]), now); ok {
			r.Windows = append(r.Windows, w)
		}
	}
	// The credits format reports one percent and a prepaid balance in cents.
	if pct := finite(cfg["creditUsagePercent"]); pct != nil && len(r.Windows) == 0 {
		clamped := math.Min(100, math.Max(0, *pct))
		r.Windows = append(r.Windows, model.Window{
			Label: "Credits", Util: &clamped, ResetsAt: isoTime(cfg["billingPeriodEnd"]),
		})
	}
	// Each product's share of the period, with the period's own reset.
	if products, ok := cfg["productUsage"].([]any); ok {
		for _, item := range products {
			product := asMap(item)
			name, pct := asString(product["product"]), finite(product["usagePercent"])
			if name == "" || pct == nil || len(r.Windows) >= maxWindows {
				continue
			}
			clamped := math.Min(100, math.Max(0, *pct))
			r.Windows = append(r.Windows, model.Window{Label: trimLabel(name), Util: &clamped, ResetsAt: isoTime(cfg["billingPeriodEnd"])})
		}
	}
	if bal := grokAmount(cfg["prepaidBalance"]); bal != nil {
		r.Credits.Enabled = true
		r.Credits.Balance = bal
	}
	return r
}

// grokAmount reads a prepaid balance. The credits payload wraps it as
// {"val": cents}. A bare number is already major units, the way the other
// providers state a balance, so dividing it would show a hundredth.
func grokAmount(v any) *float64 {
	if _, wrapped := v.(map[string]any); wrapped {
		n := finite(asMap(v)["val"])
		if n == nil {
			return nil
		}
		// Cents are whole. A fraction here is a unit mix-up, not money.
		cents := math.Round(*n)
		if cents != *n {
			return nil
		}
		dollars := cents / 100
		return &dollars
	}
	return finite(v)
}

// afterSeconds is now plus a whole number of seconds, or nil when the count
// is missing, fractional, negative, or too large for seconds to count.
func afterSeconds(now time.Time, count *float64) *time.Time {
	if count == nil || *count != math.Trunc(*count) || *count <= 0 || *count > math.MaxInt64/float64(time.Second) {
		return nil
	}
	t := now.UTC().Add(time.Duration(*count) * time.Second)
	return &t
}

func grokPeriod(cfg map[string]any, now time.Time) (model.Window, bool) {
	used := finite(first(cfg, "used_percent", "utilization", "percentage"))
	if used == nil {
		// credits format: remaining of a total.
		total := finite(first(cfg, "total", "limit"))
		left := finite(first(cfg, "remaining", "left"))
		if total != nil && left != nil && *total > 0 {
			u := (1 - *left / *total) * 100
			u = math.Min(100, math.Max(0, u))
			used = &u
		}
	}
	if used == nil {
		return model.Window{}, false
	}
	label := trimLabel(asString(first(cfg, "label", "period")))
	if label == "" {
		label = "Usage"
	}
	var resets *time.Time
	if t := isoTime(first(cfg, "resets_at", "reset_at", "period_end")); t != nil {
		resets = t
	} else if t := afterSeconds(now, finite(cfg["reset_after_seconds"])); t != nil {
		resets = t
	}
	clamped := math.Min(100, math.Max(0, *used))
	return model.Window{Label: label, Util: &clamped, ResetsAt: resets}, true
}

func parseAntigravity(obj map[string]any, now time.Time) model.Reading {
	r := model.Reading{OK: true, Windows: []model.Window{}}
	list, _ := asMap(asMap(obj["command"])["data"])["groups"].([]any)
	for _, g := range list {
		block := asMap(g)
		name := trimLabel(asString(block["name"]))
		buckets, _ := block["buckets"].([]any)
		for _, b := range buckets {
			item := asMap(b)
			remaining := finite(item["remaining_fraction"])
			if remaining == nil {
				continue
			}
			left := math.Min(1, math.Max(0, *remaining))
			used := (1 - left) * 100
			window := asString(item["window"])
			label := name
			switch window {
			case "weekly":
				label = strings.TrimSpace(name + " weekly")
			case "5h":
				label = strings.TrimSpace(name + " 5h")
			}
			if label == "" {
				label = trimLabel(asString(item["name"]))
			}
			if label == "" {
				continue
			}
			r.Windows = append(r.Windows, model.Window{Label: label, Util: &used, ResetsAt: isoTime(item["reset_time"])})
			if len(r.Windows) >= maxWindows {
				return r
			}
		}
	}
	_ = now
	return r
}

// matchTier picks the tier whose label or id appears in the plan string as a
// whole word, so "pro" does not match inside "prolite". The highest rank
// wins, and at the same rank the longer name wins, so "Pro Lite" does not
// collapse into "Pro" when both are present, and "Ultra" beats "Max".
func matchTier(p model.Provider, plan string) string {
	hay := fold(plan)
	var best model.Tier
	found := false
	for _, t := range model.Tiers(p) {
		if !mentions(hay, fold(t.ID)) && !mentions(hay, fold(t.Label)) {
			continue
		}
		if !found || t.Rank > best.Rank || (t.Rank == best.Rank && len(t.Label) > len(best.Label)) {
			best = t
			found = true
		}
	}
	if !found {
		return ""
	}
	return best.ID
}

// mentions reports whether needle occurs in hay bounded by non-letters, so
// one tier's id is not found inside a longer tier's id.
func mentions(hay, needle string) bool {
	if needle == "" {
		return false
	}
	h, n := []rune(hay), []rune(needle)
	for i := 0; i+len(n) <= len(h); i++ {
		if string(h[i:i+len(n)]) != needle {
			continue
		}
		before := i == 0 || !unicode.IsLetter(h[i-1])
		end := i + len(n)
		after := end == len(h) || !unicode.IsLetter(h[end])
		if before && after {
			return true
		}
	}
	return false
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func asString(v any) string {
	s, _ := v.(string)
	// A credential file or a vendor payload is untrusted JSON, and encoding/json
	// accepts raw invalid UTF-8. Text copied into a name, a label, or a claim
	// must be valid, so anything else reads as absent.
	if !utf8.ValidString(s) {
		return ""
	}
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func first(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func finite(v any) *float64 {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case json.Number:
		var err error
		f, err = n.Float64()
		if err != nil {
			return nil
		}
	case string:
		var err error
		f, err = strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return nil
		}
	default:
		return nil
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

func isoTime(v any) *time.Time {
	switch n := v.(type) {
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
			if t, err := time.Parse(layout, n); err == nil {
				u := t.UTC()
				return &u
			}
		}
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return unixMaybe(f)
		}
	case float64:
		return unixMaybe(n)
	}
	return nil
}

func unixMaybe(v float64) *time.Time {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return nil
	}
	sec := v
	// 1e12 is 2001-09-09 in milliseconds and year 33658 in seconds, so a
	// value at the boundary is milliseconds. A strict greater-than drops
	// that instant, and an expiry equal to it then never reads as past.
	if sec >= 1e12 {
		sec = sec / 1000
	}
	if sec > 253402300799 {
		return nil
	}
	// Round, so a millisecond timestamp does not lose up to a second. A
	// truncated expiry reads as already past when it has not.
	t := time.Unix(int64(math.Round(sec)), 0).UTC()
	return &t
}

func titleWords(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		r, size := utf8.DecodeRuneInString(p)
		if r == utf8.RuneError {
			continue
		}
		parts[i] = string(unicode.ToUpper(r)) + p[size:]
	}
	return strings.Join(parts, " ")
}

func trimLabel(s string) string {
	return model.TruncateRunes(strings.TrimSpace(s), model.MaxNameLen)
}
