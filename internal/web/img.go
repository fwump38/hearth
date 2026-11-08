package web

import (
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var thumbWidths = []int{300, 600, 1200}

func snapWidth(w int) int {
	for _, t := range thumbWidths {
		if w <= t {
			return t
		}
	}
	return thumbWidths[len(thumbWidths)-1]
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.store.Get(r.PathValue("slug"))
	if !ok || rec.Image == "" {
		http.NotFound(w, r)
		return
	}
	src := filepath.Join(s.store.Dir, rec.Image)
	fi, err := os.Stat(src)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	width, _ := strconv.Atoi(r.URL.Query().Get("w"))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if width <= 0 {
		http.ServeFile(w, r, src)
		return
	}
	width = snapWidth(width)
	cache := filepath.Join(s.thumbs, fmt.Sprintf("%s-%d-%d.jpg", rec.Slug, width, fi.ModTime().Unix()))
	if _, err := os.Stat(cache); err != nil {
		if err := makeThumb(src, cache, width); err != nil {
			// Unsupported/corrupt image: fall back to the original.
			http.ServeFile(w, r, src)
			return
		}
	}
	http.ServeFile(w, r, cache)
}

func makeThumb(src, dst string, width int) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	if b.Dx() > width {
		h := b.Dy() * width / b.Dx()
		dstImg := image.NewRGBA(image.Rect(0, 0, width, h))
		draw.CatmullRom.Scale(dstImg, dstImg.Bounds(), img, b, draw.Over, nil)
		img = dstImg
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(out, img, &jpeg.Options{Quality: 82}); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, dst)
}
