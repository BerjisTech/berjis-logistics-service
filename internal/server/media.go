package server

import (
	"context"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "image/png" // register PNG decoder for image.Decode

	"github.com/berjistech/berjis-ecosystem/logistics/service/internal/media"
	"github.com/gofiber/fiber/v2"
)

func registerMediaRoutes(app *fiber.App, opts Options) {
	// Upload multiple images -> resizes + thumbnails, returns { urls, thumbs }
	app.Post("/v1/uploads/images", func(c *fiber.Ctx) error {
		mf, err := c.MultipartForm()
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid form", "code": "INVALID_MULTIPART"})
		}
		files := mf.File["files"]
		if len(files) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "no files", "code": "NO_FILES"})
		}

		store, base, err := media.NewStore(context.Background())
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}

		urls := []string{}
		thumbs := []string{}
		// diagnostics to help users
		var (
			rejectedSize   int
			rejectedType   int
			decodeFailed   int
			storageFailed  int
			openedFailed   int
			attemptedStore int
		)
		for _, fh := range files {
			if fh.Size <= 0 || fh.Size > 10*1024*1024 {
				rejectedSize++
				continue
			}
			ct := detectContentType(fh)
			if !strings.HasPrefix(ct, "image/") {
				rejectedType++
				continue
			}
			f, err := fh.Open()
			if err != nil {
				openedFailed++
				continue
			}
			data, _ := io.ReadAll(f)
			_ = f.Close()
			full, thumb, err := media.Process(data)
			if err != nil {
				decodeFailed++
				continue
			}
			// Store
			ext := ".jpg"
			fullKey := media.DatedKey("images", ext)
			thKey := media.DatedKey("thumbs", ext)
			attemptedStore++
			fullURL, err1 := store.Put(c.Context(), fullKey, "image/jpeg", full)
			thURL, err2 := store.Put(c.Context(), thKey, "image/jpeg", thumb)
			if err1 != nil || err2 != nil {
				// Attempt runtime fallback to local storage
				if ls, lbase, lerr := media.NewLocal(); lerr == nil {
					fullURL2, err1b := ls.Put(c.Context(), fullKey, "image/jpeg", full)
					thURL2, err2b := ls.Put(c.Context(), thKey, "image/jpeg", thumb)
					if err1b == nil && err2b == nil {
						// adjust to absolute if needed for local store
						if lbase == "" && strings.HasPrefix(fullURL2, "/") {
							fullURL2 = c.BaseURL() + fullURL2
						}
						if lbase == "" && strings.HasPrefix(thURL2, "/") {
							thURL2 = c.BaseURL() + thURL2
						}
						urls = append(urls, fullURL2)
						thumbs = append(thumbs, thURL2)
						continue
					}
				}
				storageFailed++
				continue
			}
			// If local drive and MEDIA_BASE_URL not set, the store may return a path like "/media/...";
			// convert to absolute using request host so it works behind Cloudflare.
			if base == "" && strings.HasPrefix(fullURL, "/") {
				fullURL = c.BaseURL() + fullURL
			}
			if base == "" && strings.HasPrefix(thURL, "/") {
				thURL = c.BaseURL() + thURL
			}
			urls = append(urls, fullURL)
			thumbs = append(thumbs, thURL)
		}
		if len(urls) == 0 {
			// Keep a simple, user-friendly message (cloud/local issues are handled silently).
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "no valid images: only JPG/PNG/WebP up to 10MB each are supported (HEIC/HEIF not supported)", "code": "VALIDATION_FAILED"})
		}
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"urls": urls, "thumbs": thumbs}})
	})

	// Delete media by URLs (best-effort). Auth required by middleware.
	app.Post("/v1/uploads/delete", func(c *fiber.Ctx) error {
		var in struct {
			URLs []string `json:"urls"`
		}
		if err := c.BodyParser(&in); err != nil || len(in.URLs) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		deleteMediaURLs(c, in.URLs)
		return c.JSON(fiber.Map{"success": true})
	})
}

func detectContentType(fh *multipart.FileHeader) string {
	if ct := fh.Header.Get("Content-Type"); ct != "" {
		return ct
	}
	switch strings.ToLower(filepath.Ext(fh.Filename)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// deleteMediaURLs best-effort deletes local files when URLs map to the configured uploads path.
// It ignores remote/cloud URLs and never returns internal errors to the client.
func deleteMediaURLs(c *fiber.Ctx, urls []string) {
	basePath := media.UploadsPublicPath()
	dir := media.UploadsDir()
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		// Accept absolute or relative URLs
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			if pu, err := url.Parse(u); err == nil {
				u = pu.Path
			}
		}
		if !strings.HasPrefix(u, basePath+"/") && u != basePath {
			continue
		}
		rel := strings.TrimPrefix(u, basePath)
		rel = strings.TrimPrefix(rel, "/")
		fp := filepath.Join(dir, filepath.FromSlash(rel))
		_ = os.Remove(fp)
	}
}
