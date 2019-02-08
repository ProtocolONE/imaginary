package main

import (
	"fmt"
	"github.com/minio/minio-go"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type MinioOptions struct {
	Endpoint            string
	AccessKey           string
	SecretKey           string
	Bucket              string
	UseSSL              bool
}

type JwtOptions struct {
	SignatureSecret       []byte
	SignatureSecretBase64 string
	Algorithm             string
}

type CORS struct {
	AllowOrigin   string
}

type Video struct {
	TempDir       string
}

type ServerOptions struct {
	Minio              MinioOptions
	Jwt                JwtOptions
	CORS               CORS
	Video              Video

	Port               int
	Burst              int
	Concurrency        int
	HTTPCacheTTL       int
	HTTPReadTimeout    int
	HTTPWriteTimeout   int
	MaxAllowedSize     int
	Gzip               bool // deprecated
	AuthForwarding     bool
	EnableURLSource    bool
	EnablePlaceholder  bool
	EnableURLSignature bool
	URLSignatureKey    string
	Address            string
	PathPrefix         string
	APIKey             string
	Mount              string
	CertFile           string
	KeyFile            string
	Authorization      string
	Placeholder        string
	PlaceholderImage   []byte
	Endpoints          Endpoints
	AllowedOrigins     []*url.URL
}

// Endpoints represents a list of endpoint names to disable.
type Endpoints []string

// IsValid validates if a given HTTP request endpoint is valid or not.
func (e Endpoints) IsValid(r *http.Request) bool {
	parts := strings.Split(r.URL.Path, "/")
	endpoint := parts[len(parts)-1]
	for _, name := range e {
		if endpoint == name {
			return false
		}
	}
	return true
}

func Server(o ServerOptions) error {
	addr := o.Address + ":" + strconv.Itoa(o.Port)
	handler := NewLog(NewServerMux(o), os.Stdout)

	server := &http.Server{
		Addr:           addr,
		Handler:        handler,
		MaxHeaderBytes: 1 << 20,
		ReadTimeout:    time.Duration(o.HTTPReadTimeout) * time.Second,
		WriteTimeout:   time.Duration(o.HTTPWriteTimeout) * time.Second,
	}

	rand.Seed(time.Now().UnixNano())

	return listenAndServe(server, o)
}

func listenAndServe(s *http.Server, o ServerOptions) error {
	if o.CertFile != "" && o.KeyFile != "" {
		return s.ListenAndServeTLS(o.CertFile, o.KeyFile)
	}
	return s.ListenAndServe()
}

func join(o ServerOptions, route string) string {
	return path.Join(o.PathPrefix, route)
}

// NewServerMux creates a new HTTP server route multiplexer.
func NewServerMux(o ServerOptions) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(join(o, "/"), Middleware(indexController, o))
	mux.Handle(join(o, "/form"), Middleware(formController, o))
	mux.Handle(join(o, "/health"), Middleware(healthController, o))

	image := ImageMiddleware(o)
	mux.Handle(join(o, "/resize"), image(Resize))
	mux.Handle(join(o, "/fit"), image(Fit))
	mux.Handle(join(o, "/enlarge"), image(Enlarge))
	mux.Handle(join(o, "/extract"), image(Extract))
	mux.Handle(join(o, "/crop"), image(Crop))
	mux.Handle(join(o, "/smartcrop"), image(SmartCrop))
	mux.Handle(join(o, "/rotate"), image(Rotate))
	mux.Handle(join(o, "/flip"), image(Flip))
	mux.Handle(join(o, "/flop"), image(Flop))
	mux.Handle(join(o, "/thumbnail"), image(Thumbnail))
	mux.Handle(join(o, "/zoom"), image(Zoom))
	mux.Handle(join(o, "/convert"), image(Convert))
	mux.Handle(join(o, "/watermark"), image(Watermark))
	mux.Handle(join(o, "/watermarkimage"), image(WatermarkImage))
	mux.Handle(join(o, "/info"), image(Info))
	mux.Handle(join(o, "/blur"), image(GaussianBlur))
	mux.Handle(join(o, "/pipeline"), image(Pipeline))

	video := VideoMiddleware{o}
	mux.Handle(join(o, "/video"), validate(validateJWT(&video, o), o))

	return mux
}

type VideoMiddleware struct {
	opts ServerOptions
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func (m *VideoMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	// Initialize minio client object.
	opts := m.opts.Minio
	minioClient, err := minio.New(opts.Endpoint, opts.AccessKey, opts.SecretKey, opts.UseSSL)
	if err != nil {
		fmt.Printf("Failed to initialize Minio: %s", err)
		return
	}

	videoFile, header, err := r.FormFile("file")
	if err != nil {
		fmt.Printf("Invalid format: %s", err)
		return
	}

	contentType := header.Header.Get("Content-Type")

	destPath := "videos"
	if vs := r.Form["dest"]; len(vs) > 0 {
		destPath = vs[0]
	}
	fileName := header.Filename
	if destPath != "" {
		fileName = fmt.Sprintf("%s/%s", destPath, fileName)
	}

	hash := "-" + RandStringRunes(6)
	oldExt := filepath.Ext(fileName)
	newExt := oldExt
	if vs := r.Form["type"]; len(vs) > 0 && vs[0] != "" {
		newExt = "." + vs[0]
	}
	fileName = fileName[:len(fileName) - len(oldExt)] + hash + newExt

	_, err = minioClient.PutObject(opts.Bucket, fileName, videoFile, header.Size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		fmt.Printf("Eror while PutObject: %s", err)
		return
	}

	publicUrl := fmt.Sprintf(`https://%s/%s/%s`, opts.Endpoint, opts.Bucket, fileName)

	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(publicUrl))

	return
}
