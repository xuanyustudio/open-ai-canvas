package protocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Official 小力孩 limits: JSON body ≤ 128 MB, base64 expands ~×1.37 so original
// files share about 93 MB. Images max 9, audios max 3, videos max 3.
const (
	newAPIVideoTasksMaxImages     = 9
	newAPIVideoTasksMaxVideos     = 3
	newAPIVideoTasksMaxAudios     = 3
	newAPIVideoTasksMaxJSONBytes  = 128 << 20
	newAPIVideoTasksOfficialOrig  = 93 << 20
	newAPIVideoTasksDefaultOrig   = 80 << 20
	newAPIVideoTasksJSONHeadroom  = 8 << 20
	newAPIVideoTasksFetchTimeout  = 30 * time.Second
	newAPIVideoTasksMaxFetchBytes = 90 << 20
	newAPIVideoTasksStartEdge     = 2048
	newAPIVideoTasksMinEdge       = 640
	newAPIVideoTasksStartQuality  = 80
	newAPIVideoTasksMinQuality    = 50
)

var (
	newAPIVideoTasksOriginalBudget = int64(newAPIVideoTasksDefaultOrig)
	newAPIVideoTasksJSONBudget     = int64(newAPIVideoTasksMaxJSONBytes - newAPIVideoTasksJSONHeadroom)
	newAPIVideoTasksHTTPClient     = &http.Client{Timeout: newAPIVideoTasksFetchTimeout}
)

// NewAPIVideoTasksLocalLoader optionally resolves a media reference to raw
// bytes and MIME type, typically by reading a local Canvas resource file.
// Return (nil, "", nil) to fall through to HTTP GET. Never log secrets or
// signed query strings.
var NewAPIVideoTasksLocalLoader func(ctx context.Context, media MediaReference) (data []byte, mimeType string, err error)

// PrepareNewAPIVideoTasksMedia rewrites images/videos/audios into data URIs
// for the 小力孩 Video Tasks adapter. Public http(s) URLs are not left in the
// request because MiniMax cannot reliably fetch Canvas's signed public links.
// The request is mutated in place: URL is cleared and DataURL is set.
func PrepareNewAPIVideoTasksMedia(ctx context.Context, r *GenerationRequest) error {
	if r == nil {
		return nil
	}
	images, err := embedNewAPIVideoTasksMedia(ctx, r.Images, "image", newAPIVideoTasksMaxImages, true)
	if err != nil {
		return err
	}
	videos, err := embedNewAPIVideoTasksMedia(ctx, r.Videos, "video", newAPIVideoTasksMaxVideos, false)
	if err != nil {
		return err
	}
	audios, err := embedNewAPIVideoTasksMedia(ctx, r.Audios, "audio", newAPIVideoTasksMaxAudios, false)
	if err != nil {
		return err
	}
	images, videos, audios, err = fitNewAPIVideoTasksBudget(images, videos, audios, r.Prompt)
	if err != nil {
		return err
	}
	r.Images, r.Videos, r.Audios = images, videos, audios
	return nil
}

func newAPIVideoTasksDataURIs(values []MediaReference) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if item := mediaValue(value); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func embedNewAPIVideoTasksMedia(ctx context.Context, values []MediaReference, kind string, limit int, compressable bool) ([]MediaReference, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > limit {
		values = values[:limit]
	}
	out := make([]MediaReference, 0, len(values))
	for _, item := range values {
		data, mimeType, err := resolveNewAPIVideoTasksAsset(ctx, item, kind)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			continue
		}
		if mimeType == "" {
			mimeType = newAPIVideoTasksDefaultMIME(kind, data)
		}
		if compressable && kind == "image" {
			// Per-image compression is applied later against the shared budget.
			mimeType = normalizeNewAPIVideoTasksImageMIME(mimeType, data)
		}
		item.DataURL = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
		item.URL = ""
		item.MIMEType = mimeType
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["originalBytes"] = len(data)
		out = append(out, item)
	}
	return out, nil
}

func resolveNewAPIVideoTasksAsset(ctx context.Context, media MediaReference, kind string) ([]byte, string, error) {
	if data, mimeType, ok := decodeNewAPIVideoTasksDataURI(firstNonEmptyMedia(media.DataURL, media.URL)); ok {
		return data, firstNonEmptyMedia(media.MIMEType, mimeType), nil
	}
	if NewAPIVideoTasksLocalLoader != nil {
		data, mimeType, err := NewAPIVideoTasksLocalLoader(ctx, media)
		if err != nil {
			return nil, "", fmt.Errorf("读取本地参考素材失败：%w", err)
		}
		if len(data) > 0 {
			return data, firstNonEmptyMedia(media.MIMEType, mimeType), nil
		}
	}
	rawURL := strings.TrimSpace(media.URL)
	if rawURL == "" || strings.HasPrefix(rawURL, "data:") {
		return nil, "", nil
	}
	data, mimeType, err := fetchNewAPIVideoTasksURL(ctx, rawURL)
	if err != nil {
		return nil, "", err
	}
	return data, firstNonEmptyMedia(media.MIMEType, mimeType), nil
}

func fetchNewAPIVideoTasksURL(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", redactMediaError(rawURL, err)
	}
	resp, err := newAPIVideoTasksHTTPClient.Do(req)
	if err != nil {
		return nil, "", redactMediaError(rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, "", fmt.Errorf("读取参考素材失败：%s: HTTP %d", redactMediaURL(rawURL), resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, newAPIVideoTasksMaxFetchBytes+1))
	if err != nil {
		return nil, "", redactMediaError(rawURL, err)
	}
	if int64(len(data)) > newAPIVideoTasksMaxFetchBytes {
		return nil, "", fmt.Errorf("参考素材超过可读上限：%s", redactMediaURL(rawURL))
	}
	mimeType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0])
	}
	return data, mimeType, nil
}

func decodeNewAPIVideoTasksDataURI(value string) ([]byte, string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "data:") {
		return nil, "", false
	}
	header, payload, ok := strings.Cut(value[5:], ",")
	if !ok {
		return nil, "", false
	}
	mimeType := "application/octet-stream"
	if mediaType, _, found := strings.Cut(header, ";"); found || header != "" {
		if found && strings.TrimSpace(mediaType) != "" {
			mimeType = strings.TrimSpace(mediaType)
		} else if !found && strings.TrimSpace(header) != "" && !strings.EqualFold(strings.TrimSpace(header), "base64") {
			mimeType = strings.TrimSpace(header)
		}
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil {
		return nil, "", false
	}
	return data, mimeType, true
}

func fitNewAPIVideoTasksBudget(images, videos, audios []MediaReference, prompt string) ([]MediaReference, []MediaReference, []MediaReference, error) {
	if newAPIVideoTasksOriginalTotal(images, videos, audios) <= newAPIVideoTasksOriginalBudget && newAPIVideoTasksEncodedTotal(images, videos, audios, prompt) <= newAPIVideoTasksJSONBudget {
		return images, videos, audios, nil
	}
	images = compressNewAPIVideoTasksImages(images, videos, audios, prompt)
	for newAPIVideoTasksOriginalTotal(images, videos, audios) > newAPIVideoTasksOriginalBudget || newAPIVideoTasksEncodedTotal(images, videos, audios, prompt) > newAPIVideoTasksJSONBudget {
		if len(images) == 0 {
			return nil, nil, nil, fmt.Errorf("参考素材压缩后仍超过小力孩请求体上限")
		}
		// Keep earlier references (usually lead characters); drop the last extra.
		images = images[:len(images)-1]
	}
	return images, videos, audios, nil
}

func compressNewAPIVideoTasksImages(images, videos, audios []MediaReference, prompt string) []MediaReference {
	if len(images) == 0 {
		return images
	}
	edges := []int{newAPIVideoTasksStartEdge, 1600, 1280, 1024, newAPIVideoTasksMinEdge}
	qualities := []int{newAPIVideoTasksStartQuality, 70, 60, newAPIVideoTasksMinQuality}
	out := append([]MediaReference(nil), images...)
	for _, edge := range edges {
		for _, quality := range qualities {
			for i := range out {
				compressed, mimeType, ok := compressNewAPIVideoTasksImage(out[i], edge, quality)
				if !ok {
					continue
				}
				out[i].DataURL = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(compressed)
				out[i].MIMEType = mimeType
			}
			if newAPIVideoTasksOriginalTotal(out, videos, audios) <= newAPIVideoTasksOriginalBudget && newAPIVideoTasksEncodedTotal(out, videos, audios, prompt) <= newAPIVideoTasksJSONBudget {
				return out
			}
		}
	}
	return out
}

func compressNewAPIVideoTasksImage(media MediaReference, edge, quality int) ([]byte, string, bool) {
	data, mimeType, ok := decodeNewAPIVideoTasksDataURI(media.DataURL)
	if !ok || !strings.HasPrefix(mimeType, "image/") {
		return nil, "", false
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", false
	}
	scaled := scaleNewAPIVideoTasksImage(decoded, edge)
	var encoded []byte
	outMIME := "image/jpeg"
	if format == "png" && imageIsOpaqueNewAPI(scaled) && quality >= newAPIVideoTasksStartQuality && edge >= newAPIVideoTasksStartEdge {
		if pngData := encodeNewAPIVideoTasksPNG(scaled); len(pngData) > 0 && len(pngData) < len(data) {
			encoded, outMIME = pngData, "image/png"
		}
	}
	if encoded == nil {
		if !imageIsOpaqueNewAPI(scaled) {
			scaled = flattenNewAPIVideoTasksImage(scaled)
		}
		encoded = encodeNewAPIVideoTasksJPEG(scaled, quality)
		if len(encoded) == 0 {
			return nil, "", false
		}
	}
	if len(encoded) >= len(data) && edge >= newAPIVideoTasksStartEdge && quality >= newAPIVideoTasksStartQuality {
		return data, mimeType, true
	}
	return encoded, outMIME, true
}

func scaleNewAPIVideoTasksImage(source image.Image, edge int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	longest := max(width, height)
	if longest <= edge || longest <= 0 {
		return source
	}
	scale := float64(edge) / float64(longest)
	targetWidth := max(1, int(math.Round(float64(width)*scale)))
	targetHeight := max(1, int(math.Round(float64(height)*scale)))
	target := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Src, nil)
	return target
}

func flattenNewAPIVideoTasksImage(source image.Image) image.Image {
	bounds := source.Bounds()
	target := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(target, target.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(target, target.Bounds(), source, bounds.Min, draw.Over)
	return target
}

func encodeNewAPIVideoTasksPNG(source image.Image) []byte {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		return nil
	}
	return buffer.Bytes()
}

func encodeNewAPIVideoTasksJPEG(source image.Image, quality int) []byte {
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, source, &jpeg.Options{Quality: quality}); err != nil {
		return nil
	}
	return buffer.Bytes()
}

func imageIsOpaqueNewAPI(source image.Image) bool {
	if opaque, ok := source.(interface{ Opaque() bool }); ok {
		return opaque.Opaque()
	}
	bounds := source.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, alpha := source.At(x, y).RGBA(); alpha < 0xffff {
				return false
			}
		}
	}
	return true
}

func newAPIVideoTasksOriginalTotal(groups ...[]MediaReference) int64 {
	var total int64
	for _, group := range groups {
		for _, item := range group {
			if data, _, ok := decodeNewAPIVideoTasksDataURI(mediaValue(item)); ok {
				total += int64(len(data))
			}
		}
	}
	return total
}

func newAPIVideoTasksEncodedTotal(images, videos, audios []MediaReference, prompt string) int64 {
	// Approximate the JSON body: field names + quoted data URIs + prompt.
	total := int64(len(prompt) + 256)
	for _, group := range [][]MediaReference{images, videos, audios} {
		for _, item := range group {
			if value := mediaValue(item); value != "" {
				total += int64(len(value) + 8)
			}
		}
	}
	return total
}

func newAPIVideoTasksDefaultMIME(kind string, data []byte) string {
	detected := strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0])
	if detected != "" && detected != "application/octet-stream" {
		return detected
	}
	switch kind {
	case "video":
		return "video/mp4"
	case "audio":
		return "audio/mpeg"
	default:
		return "image/jpeg"
	}
}

func normalizeNewAPIVideoTasksImageMIME(mimeType string, data []byte) string {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if strings.HasPrefix(mimeType, "image/") {
		return mimeType
	}
	return newAPIVideoTasksDefaultMIME("image", data)
}

func firstNonEmptyMedia(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func redactMediaError(rawURL string, err error) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), rawURL, redactMediaURL(rawURL))
	return fmt.Errorf("读取参考素材失败：%s: %s", redactMediaURL(rawURL), message)
}

func redactMediaURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return "invalid-url"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String()
}
