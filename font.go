package mobilegles

import (
	"embed"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

var (
	systemTTF  *truetype.Font
	fontLoadMu sync.Mutex
	fontSize   float32 = 14
)

// CapHeightPx measures the 'H' cap height in px for a truetype font at
// the given em size. Returns 0 if it cannot be measured.
func CapHeightPx(f *truetype.Font, size float32) float32 {
	if f == nil || size <= 0 {
		return 0
	}
	scale := fixed.Int26_6(size * 64)
	idx := f.Index('H')
	if idx == 0 {
		return 0
	}
	var gb truetype.GlyphBuf
	if err := gb.Load(f, scale, idx, font.HintingNone); err != nil {
		return 0
	}
	top := fixed.Int26_6(0)
	for _, p := range gb.Points {
		if p.Y > top {
			top = p.Y
		}
	}
	return float32(top) / 64
}

// DescentPx returns the font's descent (distance below baseline) in px
// at the given em size. Prefers hhea metrics (what skia reports to
// unison); falls back to the global glyph bounding box.
func DescentPx(f *truetype.Font, size float32) float32 {
	if f == nil || size <= 0 {
		return 0
	}
	if m, ok := fontMetrics.Load(f); ok {
		if r := m.(fontMetricsRatios).descentRatio; r > 0 {
			return r * size
		}
	}
	scale := fixed.Int26_6(size * 64)
	b := f.Bounds(scale)
	return float32(-b.Min.Y) / 64
}

type ptInfo struct {
	x, y float64
	on   bool
}

// fontMetricsRatios carries hhea ascent/descent as fractions of
// unitsPerEm — matching what skia reports as FontMetrics, which unison's
// LineHeight/Baseline formulas are built on.
type fontMetricsRatios struct {
	ascentRatio  float32 // positive, above baseline
	descentRatio float32 // positive, below baseline
}

var fontMetrics sync.Map // *truetype.Font -> fontMetricsRatios

// parseTTFMetrics extracts hhea ascent/descent and head unitsPerEm.
func parseTTFMetrics(data []byte) fontMetricsRatios {
	var r fontMetricsRatios
	if len(data) < 16 {
		return r
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	var ascent, descent, upem float32
	for i := 0; i < numTables; i++ {
		rec := data[12+16*i : 12+16*i+16]
		if len(rec) < 16 {
			return r
		}
		tag := string(rec[0:4])
		off := int(binary.BigEndian.Uint32(rec[8:12]))
		switch tag {
		case "hhea":
			if off+10 <= len(data) {
				ascent = float32(int16(binary.BigEndian.Uint16(data[off+4 : off+6])))
				descent = float32(-int16(binary.BigEndian.Uint16(data[off+6 : off+8])))
			}
		case "head":
			if off+20 <= len(data) {
				upem = float32(int16(binary.BigEndian.Uint16(data[off+18 : off+20])))
			}
		}
	}
	if upem > 0 {
		r.ascentRatio = ascent / upem
		r.descentRatio = descent / upem
	}
	return r
}

// FontMetricsRatios returns the hhea-derived ascent/descent ratios for a
// parsed font (0 values if unknown).
func FontMetricsRatios(f *truetype.Font) fontMetricsRatios {
	if v, ok := fontMetrics.Load(f); ok {
		return v.(fontMetricsRatios)
	}
	return fontMetricsRatios{}
}

func LoadSystemFont(path string) (*truetype.Font, error) {
	fontLoadMu.Lock()
	defer fontLoadMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load font %s: %w", path, err)
	}
	f, err := truetype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse font %s: %w", path, err)
	}
	systemTTF = f
	fontMetrics.Store(f, parseTTFMetrics(data))
	return f, nil
}

func SystemTTF() *truetype.Font {
	return systemTTF
}

func LoadTTFFromBytes(data []byte) (*truetype.Font, error) {
	f, err := truetype.Parse(data)
	if err == nil {
		fontMetrics.Store(f, parseTTFMetrics(data))
	}
	return f, err
}

func LoadTTFFromFile(path string) (*truetype.Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadTTFFromBytes(data)
}

func SetSystemFont(face *truetype.Font) {
	systemTTF = face
}

// Embedded Roboto family (same files unison embeds) — resolves
// MatchFontFace("Roboto", ...) for theme parity with production.
// Faces are keyed by style suffix ("Regular", "Light", "Bold Italic", …)
// so this package stays free of enum dependencies.
var (
	embeddedFontsOnce sync.Once
	robotoFaces       map[string]*truetype.Font
)

//go:embed resources/fonts
var fontResourcesFS embed.FS

// robotoStyleFromName maps "Roboto - Bold Italic.ttf" to its style
// suffix ("Bold Italic").
func robotoStyleFromName(name string) string {
	base := name
	if i := strings.LastIndex(base, "- "); i >= 0 {
		base = base[i+2:]
	}
	return strings.TrimSuffix(base, ".ttf")
}

func registerEmbeddedRoboto() {
	embeddedFontsOnce.Do(func() {
		robotoFaces = make(map[string]*truetype.Font)
		entries, err := fontResourcesFS.ReadDir("resources/fonts")
		if err != nil {
			return
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".ttf") {
				continue
			}
			data, err := fontResourcesFS.ReadFile("resources/fonts/" + e.Name())
			if err != nil {
				continue
			}
			f, err := LoadTTFFromBytes(data)
			if err != nil {
				continue
			}
			robotoFaces[robotoStyleFromName(e.Name())] = f
		}
	})
}

// EmbeddedRobotoFace returns the embedded Roboto face for a style
// suffix ("Light", "Bold", "Bold Italic", …) with "Regular" fallback.
func EmbeddedRobotoFace(style string) *truetype.Font {
	registerEmbeddedRoboto()
	if f, ok := robotoFaces[style]; ok {
		return f
	}
	return robotoFaces["Regular"]
}

func fontSizeInPixels(size float32) fixed.Int26_6 {
	return fixed.Int26_6(size * 64.0)
}
