package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/assets"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPublicResourceSignedHEADReturnsHeadersWithoutBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if err := os.WriteFile(filepath.Join(dataDir, ".settings-key"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	objectKey := "users/user-1/image/ref.png"
	payload := []byte("png-bytes")
	if err := os.MkdirAll(filepath.Join(dataDir, "resources", filepath.Dir(objectKey)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "resources", objectKey), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{
		ID: "resource-head", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: objectKey, MimeType: "image/png", Size: int64(len(payload)),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), service.New(repository.New(db), dataDir))

	expires := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	// 签名对象与 signedResourceAccessURL 一致：id + "\n" + variant + "\n" + expires。
	// 未带 variant 时 NormalizeAccessOptions 会默认 original。
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(resource.ID + "\n" + string(assets.VariantOriginal) + "\n" + expires))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	signedPath := "/api/public/resources/" + resource.ID + "/file?expires=" + expires + "&signature=" + signature + "&variant=" + string(assets.VariantOriginal)

	call := func(method, path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}

	if w := call(http.MethodHead, "/api/public/resources/"+resource.ID+"/file"); w.Code == http.StatusOK || w.Code == http.StatusNotFound {
		t.Fatalf("unsigned HEAD status = %d, want auth failure (not 200 or missing-route 404)", w.Code)
	} else if w.Code != http.StatusForbidden {
		t.Fatalf("unsigned HEAD status = %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodGet, "/api/public/resources/"+resource.ID+"/file"); w.Code == http.StatusOK {
		t.Fatal("unsigned GET must not succeed")
	} else if w.Code != http.StatusForbidden {
		t.Fatalf("unsigned GET status = %d %s", w.Code, w.Body.String())
	}
	tampered := strings.Replace(signedPath, "signature="+signature, "signature="+signature+"x", 1)
	if w := call(http.MethodHead, tampered); w.Code != http.StatusForbidden {
		t.Fatalf("altered HEAD status = %d %s", w.Code, w.Body.String())
	}

	head := call(http.MethodHead, signedPath)
	if head.Code != http.StatusOK {
		t.Fatalf("signed HEAD status = %d %s", head.Code, head.Body.String())
	}
	if head.Body.Len() != 0 {
		t.Fatalf("signed HEAD wrote %d body bytes", head.Body.Len())
	}
	if got := head.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("signed HEAD content-type = %q", got)
	}
	if got := head.Header().Get("Content-Length"); got != strconv.Itoa(len(payload)) {
		t.Fatalf("signed HEAD content-length = %q", got)
	}

	get := call(http.MethodGet, signedPath)
	if get.Code != http.StatusOK {
		t.Fatalf("signed GET status = %d %s", get.Code, get.Body.String())
	}
	if get.Body.String() != string(payload) {
		t.Fatalf("signed GET body = %q", get.Body.String())
	}
}
