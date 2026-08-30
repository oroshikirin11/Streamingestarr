package router

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/CAFxX/httpcompression"
	"github.com/go-chi/chi/v5"
	chiMW "github.com/go-chi/chi/v5/middleware"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"streamingestarr/config"
	"streamingestarr/core/chat"
	"streamingestarr/webserver/handlers"
	"streamingestarr/webserver/router/middleware"
)

// Start starts the router for the http, ws, and rtmp.
func Start(enableVerboseLogging bool) error {
	// @behlers New Router
	r := chi.NewRouter()

	// Middlewares
	if enableVerboseLogging {
		r.Use(chiMW.RequestLogger(&chiMW.DefaultLogFormatter{Logger: log.StandardLogger(), NoColor: true}))
	}
	r.Use(chiMW.Recoverer)

	// Everything behind the viewer gate — including HLS and the chat
	// websocket. Must be registered before any routes.
	r.Use(middleware.RequireViewerAccess)

	// The gate's own surface: login, first-run setup, session management.
	r.Get("/login", handlers.GetLoginPage)
	r.Get("/setup", handlers.GetSetupPage)
	r.Post("/api/auth/login", handlers.PostAuthLogin)
	r.Post("/api/auth/setup", handlers.PostAuthSetup)
	r.Post("/api/auth/logout", handlers.PostAuthLogout)
	r.Get("/api/auth/status", handlers.GetAuthStatus)
	r.Post("/api/admin/config/viewerlogin", middleware.RequireAdminAuth(handlers.SetViewerLogin))
	r.Post("/api/admin/config/chat/namereservationdays", middleware.RequireAdminAuth(handlers.SetChatNameReservationDays))
	r.Post("/api/admin/config/video/segmentformat", middleware.RequireAdminAuth(handlers.SetVideoSegmentFormat))
	r.Post("/api/admin/config/srt/enabled", middleware.RequireAdminAuth(handlers.SetSRTEnabled))
	r.Post("/api/admin/config/srt/port", middleware.RequireAdminAuth(handlers.SetSRTPort))

	addStaticFileEndpoints(r)

	// websocket
	r.HandleFunc("/ws", chat.HandleClientConnection)

	// serve files
	fs := http.FileServer(http.Dir(config.PublicFilesPath))
	r.Handle("/public/*", http.StripPrefix("/public/", fs))

	// Return HLS video. URLs are channel-scoped (/hls/<channel>/...);
	// unscoped paths resolve to the default channel.
	r.HandleFunc("/hls/*", handlers.HandleHLSRequest)

	// Channel-scoped viewer pages. One theater today: the root serves it,
	// and /t/<channel> is the scoped address the multi-channel future uses.
	r.HandleFunc("/t/*", handlers.ViewerAppHandler)

	// The admin web app (Svelte, same bundle as the viewer).
	r.HandleFunc("/admin", middleware.RequireAdminAuth(handlers.ViewerAppHandler))
	r.HandleFunc("/admin/*", middleware.RequireAdminAuth(handlers.ViewerAppHandler))

	// The primary web app — the Svelte viewer, with legacy-bundle fallback
	// for the admin app's assets.
	r.HandleFunc("/*", handlers.ViewerAppHandler)

	// mount the api
	r.Mount("/api/", handlers.New().Handler())

	// Create a custom mux handler to intercept the /debug/vars endpoint.
	// This is a hack because Prometheus enables this endpoint by default
	// due to its use of expvar and we do not want this exposed.
	h2s := &http2.Server{}
	http2Handler := h2c.NewHandler(r, h2s)
	m := http.NewServeMux()

	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/debug/vars":
			w.WriteHeader(http.StatusNotFound)
			return
		case "/embed/chat/", "/embed/chat":
			// Redirect /embed/chat
			http.Redirect(w, r, "/embed/chat/readonly", http.StatusTemporaryRedirect)
		default:
			http2Handler.ServeHTTP(w, r)
		}
	})

	port := config.WebServerPort
	ip := config.WebServerIP

	compress, _ := httpcompression.DefaultAdapter() // Use the default configuration
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", ip, port),
		ReadHeaderTimeout: 4 * time.Second,
		Handler:           compress(m),
	}

	if ip != "0.0.0.0" {
		log.Infof("Web server is listening at %s:%d.", ip, port)
	} else {
		log.Infof("Web server is listening on port %d.", port)
	}
	log.Infoln("Configure this server by visiting /admin.")

	return server.ListenAndServe()
}

func addStaticFileEndpoints(r chi.Router) {
	// Images
	r.HandleFunc("/thumbnail.jpg", handlers.GetThumbnail)
	r.HandleFunc("/preview.gif", handlers.GetPreview)
	r.HandleFunc("/logo", handlers.GetLogo)
	r.HandleFunc("/favicon.ico", handlers.GetFavicon)
	// return a logo that's compatible with external social networks
	r.HandleFunc("/logo/external", handlers.GetCompatibleLogo)

	// Custom Javascript
	r.HandleFunc("/customjavascript", handlers.ServeCustomJavascript)

	// robots.txt
	r.HandleFunc("/robots.txt", handlers.GetRobotsDotTxt)

	// Return a single emoji image.
	emojiDir := config.EmojiDir
	if !strings.HasSuffix(emojiDir, "*") {
		emojiDir += "*"
	}
	r.HandleFunc(emojiDir, handlers.GetCustomEmojiImage)
}
