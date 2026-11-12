package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"rsc.io/qr"

	"github.com/guettli/vibetype/internal/store"
)

// qrSVG renders text as a QR code SVG with the standard quiet zone of 4 modules.
func qrSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := code.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := range code.Size {
		for x := range code.Size {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}

// textQR serves a QR code of the text's share link (for the projector in class).
func (s *Server) textQR(w http.ResponseWriter, r *http.Request) {
	t, err := s.cfg.Store.GetText(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "text not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	svg, err := qrSVG(s.cfg.Origin + "/t/" + t.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write([]byte(svg))
}
