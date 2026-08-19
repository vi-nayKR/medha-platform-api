package docs

import (
	"embed"
	"net/http"
)

//go:embed swagger.yaml swagger.json custom-swagger.css
var specFS embed.FS

// YamlHandler returns an http.HandlerFunc that serves the OpenAPI YAML spec file.
func YamlHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := specFS.ReadFile("swagger.yaml")
		if err != nil {
			http.Error(w, "yaml spec not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// JSONHandler returns an http.HandlerFunc that serves the OpenAPI JSON spec file.
func JSONHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := specFS.ReadFile("swagger.json")
		if err != nil {
			http.Error(w, "json spec not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// SpecHandler is an alias for YamlHandler for backward compatibility
func SpecHandler() http.HandlerFunc {
	return YamlHandler()
}

// CustomStylesHandler returns an http.HandlerFunc that serves the custom CSS file.
func CustomStylesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := specFS.ReadFile("custom-swagger.css")
		if err != nil {
			http.Error(w, "css not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// SwaggerUIHandler returns an http.HandlerFunc that serves Swagger UI
// using the public CDN (no local assets needed).
func UIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(swaggerHTML))
	}
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="en" class="dark-theme">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Medha API — Swagger UI</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
    <style>
        html {
            background: #ffffff;
            transition: filter 0.2s ease;
        }
        html.dark-theme {
            filter: invert(1) hue-rotate(180deg);
        }
        /* Restore original colors of logos, images, icons, and topbar */
        html.dark-theme img,
        html.dark-theme svg,
        html.dark-theme .logo,
        html.dark-theme .topbar {
            filter: invert(1) hue-rotate(180deg);
        }
        body { margin: 0; }
        .theme-toggle-btn {
            position: fixed;
            top: 10px;
            right: 20px;
            z-index: 9999;
            background: #f0f0f0;
            border: 1px solid #ccc;
            border-radius: 50%;
            width: 36px;
            height: 36px;
            font-size: 18px;
            cursor: pointer;
            display: flex;
            align-items: center;
            justify-content: center;
            box-shadow: 0 2px 5px rgba(0,0,0,0.15);
            transition: all 0.2s ease;
        }
        .theme-toggle-btn:hover {
            transform: scale(1.08);
        }
        html.dark-theme .theme-toggle-btn {
            background: #333;
            border-color: #555;
            color: #fff;
            filter: invert(1) hue-rotate(180deg);
        }
    </style>
</head>
<body>
    <button id="theme-toggle" class="theme-toggle-btn">☀️</button>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
        SwaggerUIBundle({
            url: "/api/docs/swagger.yaml",
            dom_id: '#swagger-ui',
            deepLinking: true,
            defaultModelsExpandDepth: -1,
            defaultModelRendering: 'example',
            presets: [
                SwaggerUIBundle.presets.apis,
                SwaggerUIBundle.SwaggerUIStandalonePreset
            ],
            layout: "BaseLayout"
        });

        // Theme Toggle Script
        const htmlEl = document.documentElement;
        const toggleBtn = document.getElementById('theme-toggle');

        // Check stored theme or default to dark
        const savedTheme = localStorage.getItem('swagger-theme');
        if (savedTheme === 'light') {
            htmlEl.classList.remove('dark-theme');
            toggleBtn.innerText = '🌙';
        } else {
            htmlEl.classList.add('dark-theme');
            toggleBtn.innerText = '☀️';
        }

        toggleBtn.addEventListener('click', () => {
            if (htmlEl.classList.contains('dark-theme')) {
                htmlEl.classList.remove('dark-theme');
                toggleBtn.innerText = '🌙';
                localStorage.setItem('swagger-theme', 'light');
            } else {
                htmlEl.classList.add('dark-theme');
                toggleBtn.innerText = '☀️';
                localStorage.setItem('swagger-theme', 'dark');
            }
        });
    </script>
</body>
</html>`
