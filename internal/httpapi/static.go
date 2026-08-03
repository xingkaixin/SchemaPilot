package httpapi

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func (server *Server) staticHandler() http.Handler {
	if server.assets == nil {
		return http.NotFoundHandler()
	}

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requested := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
		if requested == "." || requested == "" {
			requested = "index.html"
		}
		content, err := fs.ReadFile(server.assets, requested)
		if err != nil {
			content, err = fs.ReadFile(server.assets, "index.html")
			requested = "index.html"
		}
		if err != nil {
			http.NotFound(response, request)
			return
		}
		if contentType := mime.TypeByExtension(path.Ext(requested)); contentType != "" {
			response.Header().Set("Content-Type", contentType)
		}
		if requested == "index.html" {
			response.Header().Set("Cache-Control", "no-cache")
		} else {
			response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		_, _ = response.Write(content)
	})
}
