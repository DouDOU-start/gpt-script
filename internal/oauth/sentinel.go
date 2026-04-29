package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sentinelFrameReferer = "https://sentinel.openai.com/backend-api/sentinel/frame.html?sv=20260416a1b2"

func BuildSentinelToken(session *Session, deviceID, flow, userAgent, locale string) string {
	token, err := requestSentinelToken(session, deviceID, flow, userAgent, locale)
	if err == nil && strings.TrimSpace(token) != "" {
		return token
	}
	payload := map[string]any{"p": "", "t": "", "c": "", "id": deviceID, "flow": flow}
	data, _ := json.Marshal(payload)
	return string(data)
}

func requestSentinelToken(session *Session, deviceID, flow, userAgent, locale string) (string, error) {
	gen := newSentinelGenerator(deviceID, userAgent, locale)
	reqPayload := map[string]any{
		"p":    gen.requirementsToken(),
		"id":   deviceID,
		"flow": flow,
	}
	raw, _ := json.Marshal(reqPayload)
	headers := http.Header{}
	headers.Set("Content-Type", "text/plain;charset=UTF-8")
	headers.Set("Referer", sentinelFrameReferer)
	headers.Set("User-Agent", userAgent)
	res, err := session.PostText("https://sentinel.openai.com/backend-api/sentinel/req", string(raw), headers)
	if err != nil {
		return "", err
	}
	if err := res.RaiseForStatus(); err != nil {
		return "", err
	}
	var challenge struct {
		Token       string `json:"token"`
		ProofOfWork struct {
			Required   bool   `json:"required"`
			Seed       string `json:"seed"`
			Difficulty string `json:"difficulty"`
		} `json:"proofofwork"`
		Turnstile struct {
			Required bool `json:"required"`
		} `json:"turnstile"`
	}
	if err := res.JSON(&challenge); err != nil {
		return "", err
	}
	pow := gen.requirementsToken()
	if challenge.ProofOfWork.Required && challenge.ProofOfWork.Seed != "" {
		pow = gen.powToken(challenge.ProofOfWork.Seed, firstNonEmpty(challenge.ProofOfWork.Difficulty, "0"))
	}
	payload := map[string]any{
		"p":    pow,
		"t":    "",
		"c":    strings.TrimSpace(challenge.Token),
		"id":   deviceID,
		"flow": flow,
	}
	data, _ := json.Marshal(payload)
	return string(data), nil
}

type sentinelGenerator struct {
	deviceID          string
	userAgent         string
	locale            string
	sid               string
	screenWidth       int
	reactContainerKey string
}

func newSentinelGenerator(deviceID, userAgent, locale string) *sentinelGenerator {
	widths := []int{1920, 2560, 1680, 1366, 1440, 1536, 1600, 3840, 3440, 2880, 1280, 1024, 3163, 1512}
	return &sentinelGenerator{
		deviceID:          deviceID,
		userAgent:         firstNonEmpty(userAgent, defaultUserAgent),
		locale:            firstNonEmpty(locale, "en-US"),
		sid:               randomToken(16),
		screenWidth:       widths[rand.Intn(len(widths))],
		reactContainerKey: "__reactContainer$" + randomStringFrom("abcdefghijklmnopqrstuvwxyz0123456789", 13),
	}
}

func (g *sentinelGenerator) config(nonce int, elapsedMs float64) []any {
	primary := strings.ReplaceAll(g.locale, "_", "-")
	base := primary
	if cut := strings.IndexAny(primary, "-_"); cut > 0 {
		base = primary[:cut]
	}
	languages := primary
	if strings.ToLower(base) != strings.ToLower(primary) {
		languages = primary + "," + base
	}
	return []any{
		g.screenWidth,
		time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT+0000 (Coordinated Universal Time)"),
		4294967296,
		nonce,
		g.userAgent,
		"https://sentinel.openai.com/backend-api/sentinel/sdk.js",
		nil,
		primary,
		languages,
		rand.Intn(7) + 2,
		"scheduling−[object Scheduling]",
		g.reactContainerKey,
		"onscrollend",
		elapsedMs,
		g.sid,
		"",
		10,
		float64(time.Now().UnixNano()) / float64(time.Millisecond),
		0, 0, 0, 0, 0, 0, 0,
	}
}

func (g *sentinelGenerator) requirementsToken() string {
	return "gAAAAAC" + encodeSentinelJSON(g.config(1, rand.Float64()*4900+100))
}

func (g *sentinelGenerator) powToken(seed, difficulty string) string {
	start := time.Now()
	for nonce := 0; nonce < 500000; nonce++ {
		payload := encodeSentinelJSON(g.config(nonce, float64(time.Since(start).Microseconds())/1000))
		if strings.HasPrefix(hashFNV1a(seed+payload), difficulty) || hashFNV1a(seed + payload)[:lenSafe(difficulty)] <= difficulty {
			return "gAAAAAB" + payload + "~S"
		}
	}
	return "gAAAAABwQ8Lk5FbGpA2NcR9dShT6gYjU7VxZ4D" + encodeSentinelJSON(nil)
}

func encodeSentinelJSON(v any) string {
	data, _ := json.Marshal(v)
	return base64.StdEncoding.EncodeToString(data)
}

func hashFNV1a(text string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(text))
	value := h.Sum32()
	value ^= value >> 16
	value *= 2246822507
	value ^= value >> 13
	value *= 3266489909
	value ^= value >> 16
	return fmt.Sprintf("%08x", value)
}

func lenSafe(s string) int {
	if len(s) > 8 {
		return 8
	}
	return len(s)
}

func DatadogTraceHeaders() http.Header {
	traceID := rand.Uint64()
	parentID := rand.Uint64()
	headers := http.Header{}
	headers.Set("Traceparent", fmt.Sprintf("00-0000000000000000%016x-%016x-01", traceID, parentID))
	headers.Set("Tracestate", "dd=s:1;o:rum")
	headers.Set("X-Datadog-Origin", "rum")
	headers.Set("X-Datadog-Parent-Id", strconv.FormatUint(parentID, 10))
	headers.Set("X-Datadog-Sampling-Priority", "1")
	headers.Set("X-Datadog-Trace-Id", strconv.FormatUint(traceID, 10))
	return headers
}
