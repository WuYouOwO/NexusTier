package webui

import (
	"embed"
	"net/http"
)

//go:embed assets/*
var assets embed.FS

func Handler() http.Handler {
	index := mustRead("assets/index.html")
	styles := mustRead("assets/styles.css")
	script := mustRead("assets/app.js")
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Cache-Control", "no-store")
		switch request.URL.Path {
		case "/":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write(index)
		case "/assets/styles.css":
			writer.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = writer.Write(styles)
		case "/assets/app.js":
			writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = writer.Write(script)
		default:
			http.NotFound(writer, request)
		}
	})
}

func mustRead(name string) []byte {
	contents, err := assets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return contents
}
