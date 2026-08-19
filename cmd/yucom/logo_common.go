package main

import (
	"image"
	"image/color"
	"math"
)

// newYUCOMLogoImage draws the original YUCOM mark without third-party icon
// assets. Three serial data paths form the functional symbol while the short
// roof line adds only a restrained flying-eave silhouette.
func newYUCOMLogoImage(size int) *image.RGBA {
	const supersample = 4
	hiSize := size * supersample
	canvas := image.NewRGBA(image.Rect(0, 0, hiSize, hiSize))
	radius := float64(7*supersample) * float64(size) / 32

	for y := 0; y < hiSize; y++ {
		for x := 0; x < hiSize; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			cx := math.Max(radius, math.Min(float64(hiSize)-radius, fx))
			cy := math.Max(radius, math.Min(float64(hiSize)-radius, fy))
			if (fx-cx)*(fx-cx)+(fy-cy)*(fy-cy) > radius*radius {
				continue
			}
			t := float64(x+y) / float64(2*(hiSize-1))
			canvas.SetRGBA(x, y, color.RGBA{
				R: uint8(37 + 42*t),
				G: uint8(99 - 29*t),
				B: uint8(235 - 6*t),
				A: 255,
			})
		}
	}

	s := float64(hiSize) / 32
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	line := func(x1, y1, x2, y2, width float64) {
		paintLine(canvas, x1*s, y1*s, x2*s, y2*s, width*s, white)
	}
	dot := func(x, y, radius float64) {
		paintCircle(canvas, x*s, y*s, radius*s, white)
	}

	// Restrained eave accent.
	line(5.5, 11, 9, 11, 1.45)
	line(9, 11, 16, 5.8, 1.45)
	line(16, 5.8, 23, 11, 1.45)
	line(23, 11, 26.5, 11, 1.45)
	line(5, 12.6, 27, 12.6, 1.45)

	// Three serial lanes and their endpoints.
	for _, y := range []float64{17.2, 21.8, 26.4} {
		dot(8, y, 1.35)
		dot(24, y, 1.35)
	}
	line(10.2, 17.2, 13, 17.2, 1.55)
	line(13, 17.2, 17, 20.2, 1.55)
	line(17, 20.2, 21.8, 20.2, 1.55)
	line(10.2, 21.8, 21.8, 21.8, 1.55)
	line(10.2, 26.4, 13, 26.4, 1.55)
	line(13, 26.4, 17, 23.4, 1.55)
	line(17, 23.4, 21.8, 23.4, 1.55)

	result := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var rgba [4]int
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					index := canvas.PixOffset(x*supersample+sx, y*supersample+sy)
					for channel := range rgba {
						rgba[channel] += int(canvas.Pix[index+channel])
					}
				}
			}
			result.SetRGBA(x, y, color.RGBA{
				R: uint8(rgba[0] / (supersample * supersample)),
				G: uint8(rgba[1] / (supersample * supersample)),
				B: uint8(rgba[2] / (supersample * supersample)),
				A: uint8(rgba[3] / (supersample * supersample)),
			})
		}
	}
	return result
}

func paintCircle(canvas *image.RGBA, cx, cy, radius float64, fill color.RGBA) {
	minX := max(0, int(math.Floor(cx-radius)))
	maxX := min(canvas.Bounds().Dx()-1, int(math.Ceil(cx+radius)))
	minY := max(0, int(math.Floor(cy-radius)))
	maxY := min(canvas.Bounds().Dy()-1, int(math.Ceil(cy+radius)))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= radius*radius {
				canvas.SetRGBA(x, y, fill)
			}
		}
	}
}

func paintLine(canvas *image.RGBA, x1, y1, x2, y2, width float64, stroke color.RGBA) {
	dx, dy := x2-x1, y2-y1
	lengthSquared := dx*dx + dy*dy
	if lengthSquared == 0 {
		paintCircle(canvas, x1, y1, width/2, stroke)
		return
	}
	radius := width / 2
	minX := max(0, int(math.Floor(math.Min(x1, x2)-radius)))
	maxX := min(canvas.Bounds().Dx()-1, int(math.Ceil(math.Max(x1, x2)+radius)))
	minY := max(0, int(math.Floor(math.Min(y1, y2)-radius)))
	maxY := min(canvas.Bounds().Dy()-1, int(math.Ceil(math.Max(y1, y2)+radius)))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			t := ((px-x1)*dx + (py-y1)*dy) / lengthSquared
			t = math.Max(0, math.Min(1, t))
			nearestX, nearestY := x1+t*dx, y1+t*dy
			distanceX, distanceY := px-nearestX, py-nearestY
			if distanceX*distanceX+distanceY*distanceY <= radius*radius {
				canvas.SetRGBA(x, y, stroke)
			}
		}
	}
}
