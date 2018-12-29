package main

import (
	"encoding/gob"
	"fmt"
	"flag"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	_ "image/gif"
	_ "image/jpeg"
	"math"
	"os"
	"time"

	"github.com/ungerik/go-cairo"
	"github.com/veandco/go-sdl2/sdl"

	"github.com/usedbytes/mini_mouse/ui/widget"
)

var bench bool
var left *widget.ImageWidget
var right *widget.ImageWidget
var final *widget.ImageWidget

func init() {
	gob.Register(&image.NRGBA{})
	gob.Register(&image.Gray{})

	const (
		defaultBench = false
		usageBench   = "Measure drawing time"
	)

	flag.BoolVar(&bench, "b", defaultBench, usageBench)
}

func absdiff_uint8(a, b uint8) int {
	if a < b {
		return int(b - a)
	} else {
		return int(a - b)
	}
}

func deltaC(a, b color.NRGBA) uint8 {
	deltaR := float64(absdiff_uint8(a.R, b.R))
	deltaG := float64(absdiff_uint8(a.G, b.G))
	deltaB := float64(absdiff_uint8(a.B, b.B))

	deltaC := math.Sqrt( (2 * deltaR * deltaR) +
			    (4 * deltaG * deltaG) +
			    (3 * deltaB * deltaB) +
			    (deltaR * ((deltaR * deltaR) - (deltaB * deltaB)) / 256.0))

	return uint8(deltaC)
}

func rgb(in color.RGBA) color.NRGBA {
	rNon := uint8(float64(in.R) * 255.0 / float64(in.A))
	gNon := uint8(float64(in.G) * 255.0 / float64(in.A))
	bNon := uint8(float64(in.B) * 255.0 / float64(in.A))

	return color.NRGBA{rNon, gNon, bNon, 0xff}
}

func rgb64(in color.RGBA64) color.NRGBA {
	rNon := uint8(float64(in.R) * 255.0 / float64(in.A))
	gNon := uint8(float64(in.G) * 255.0 / float64(in.A))
	bNon := uint8(float64(in.B) * 255.0 / float64(in.A))

	return color.NRGBA{rNon, gNon, bNon, 0xff}
}

func absdiff(a, b color.Color) color.Gray {
	switch aPix := a.(type) {
	case color.NRGBA:
		bPix := b.(color.NRGBA)
		diff := absdiff_uint8(aPix.R, bPix.R) +
			absdiff_uint8(aPix.G, bPix.G) +
			absdiff_uint8(aPix.B, bPix.B)
		return color.Gray{uint8(float64(diff) / (3))}
	case color.RGBA:
		bPix := b.(color.RGBA)

		aNon := uint8(float64(aPix.R) * 255.0 / float64(aPix.A))
		bNon := uint8(float64(bPix.R) * 255.0 / float64(bPix.A))
		diff := absdiff_uint8(aNon, bNon)

		aNon = uint8(float64(aPix.G) * 255.0 / float64(aPix.A))
		bNon = uint8(float64(bPix.G) * 255.0 / float64(bPix.A))
		diff += absdiff_uint8(aNon, bNon)

		aNon = uint8(float64(aPix.B) * 255.0 / float64(aPix.A))
		bNon = uint8(float64(bPix.B) * 255.0 / float64(bPix.A))
		diff += absdiff_uint8(aNon, bNon)

		return color.Gray{uint8(float64(diff) / (3))}
	case color.Gray:
		bPix := b.(color.Gray)
		return color.Gray{uint8(absdiff_uint8(aPix.Y, bPix.Y))}
	}

	return color.Gray{0}
}

func wikidiff(a, b color.Color) color.Gray {
	switch aPix := a.(type) {
	case color.NRGBA:
		bPix := b.(color.NRGBA)

		return color.Gray{deltaC(aPix, bPix)}
	case color.RGBA:
		bPix := b.(color.RGBA)

		return color.Gray{deltaC(rgb(aPix), rgb(bPix))}
	case color.RGBA64:
		bPix := b.(color.RGBA64)

		return color.Gray{deltaC(rgb64(aPix), rgb64(bPix))}
	case color.Gray:
		bPix := b.(color.Gray)
		diff := absdiff_uint8(aPix.Y, bPix.Y)
		return color.Gray{uint8(diff)}
	default:
		panic(fmt.Sprintf("Unknown color type %#v", aPix))
	}

	return color.Gray{0}
}

func diffImage(in image.Image) *image.Gray {
	w, h := in.Bounds().Dx(), in.Bounds().Dy()

	out := image.NewGray(image.Rect(0, 0, w, h - 1))
	for x := 0; x < w; x++ {
		for y := 0; y < h - 1; y++ {
			diff := wikidiff(in.At(x, y), in.At(x, y + 1))
			//diff := absdiff(in.At(x, y), in.At(x, y + 1))
			out.Set(x, y, diff)
		}
	}

	return out
}

func findMinMaxColwise(img *image.Gray) []image.Point {
	ret := make([]image.Point, img.Bounds().Dx())
	w, h := img.Bounds().Dx(), img.Bounds().Dy()

	for i := 0; i < w; i++ {
		var min, max uint8 = 255, 0
		for j := 0; j < h; j++ {
			pix := img.At(i, j).(color.Gray)
			if pix.Y < min {
				min = pix.Y
			}
			if pix.Y > max {
				max = pix.Y
			}
		}
		ret[i] = image.Pt(int(max), int(min))
	}

	return ret
}

func expandContrastColWise(img *image.Gray, minMax []image.Point) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()

	for i := 0; i < w; i++ {
		scale := 255.0 / float32(minMax[i].X - minMax[i].Y)

		for j := 0; j < h; j++ {
			pix := img.At(i, j).(color.Gray)
			newVal := float32(pix.Y - uint8(minMax[i].Y)) * scale
			img.Set(i, j, color.Gray{uint8(newVal)})
		}
	}
}

func threshold(img *image.Gray) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()

	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			pix := img.At(j, i).(color.Gray)
			if pix.Y >= 128 {
				img.Set(j, i, color.Gray{255})
			} else {
				img.Set(j, i, color.Gray{0})
			}
		}
	}
}

func sumLine(img *image.Gray, line int) int {
	w := img.Bounds().Dx()
	sum := 0

	for x := 0; x < w; x++ {
		if img.At(x, line).(color.Gray).Y > 0 {
			sum++
		}
	}

	return sum
}

func findLines(img *image.Gray) *image.Gray {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	stripeH := int(float64(h) / 16)
	scale := 255.0 / float64(w * stripeH)

	sums := make([]int, h)
	for y := 0; y < h; y++ {
		sums[y] = sumLine(img, y)
	}

	out := image.NewGray(image.Rect(0, 0, 1, h))
	for y := h - (stripeH / 2) - 1; y > (stripeH / 2) + 1; y-- {
		sum := 0
		for j := 0; j < stripeH; j++ {
			sum += sums[y + (stripeH / 2) - j]
		}

		out.Set(0, y, color.Gray{uint8(float64(sum) * scale)})
	}

	return out
}

func updateImage(fname string) {
	inFile, err := os.Open(fname)
	if err != nil {
		panic(err)
	}

	img, _, err := image.Decode(inFile)
	if err != nil {
		panic(err)
	}

	left.SetImage(img)

	grad := diffImage(img)
	minMax := findMinMaxColwise(grad)
	expandContrastColWise(grad, minMax)
	threshold(grad)

	summed := findLines(grad)
	final.SetImage(summed)

	minMax = findMinMaxColwise(summed)
	expandContrastColWise(summed, minMax)
	threshold(summed)

	mod := image.NewRGBA(grad.Bounds())
	draw.Draw(mod, grad.Bounds(), grad, image.ZP, draw.Src)

	red := &image.Uniform{color.RGBA{0x80, 0, 0, 0x80}}
	for y := 0; y < summed.Bounds().Dy(); y++ {
		if summed.At(0, y).(color.Gray).Y > 0 {
			rect := image.Rect(0, y, img.Bounds().Dy(), y + 1)
			draw.Draw(mod, rect, red, image.ZP, draw.Over)
		}
	}

	right.SetImage(mod)
}

func main() {
	flag.Parse()

	if err := sdl.Init(sdl.INIT_EVERYTHING); err != nil {
		panic(err)
	}
	defer sdl.Quit()

	windowW := 1150
	windowH := 600

	window, err := sdl.CreateWindow("Mini Mouse", sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED,
		int32(windowW), int32(windowH), sdl.WINDOW_SHOWN)
	if err != nil {
		panic(err)
	}
	defer window.Destroy()

	sdlSurface, err := window.GetSurface()
	if err != nil {
		panic(err)
	}

	cairoSurface := cairo.NewSurfaceFromData(sdlSurface.Data(), cairo.FORMAT_ARGB32, int(sdlSurface.W), int(sdlSurface.H), int(sdlSurface.Pitch));

	left = widget.NewImageWidget()
	right = widget.NewImageWidget()
	final = widget.NewImageWidget()

	idx := 0
	updateImage(flag.Arg(idx))

	running := true
	tick := time.NewTicker(16 * time.Millisecond)
	for running {
		<-tick.C
		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch ev := event.(type) {
			case *sdl.QuitEvent:
				println("Quit")
				running = false
				break
			case *sdl.KeyboardEvent:
				if ev.State == 0 {
					if ev.Keysym.Sym == 'q' {
						println("Quit")
						running = false
					}

					if ev.Keysym.Sym == sdl.K_LEFT {
						idx -= 1
						if idx < 0 {
							idx = flag.NArg() - 1
						}
					}

					if ev.Keysym.Sym == sdl.K_RIGHT {
						idx += 1
						if idx >= flag.NArg() {
							idx = 0
						}
					}

					updateImage(flag.Arg(idx))
				}
			}
		}

		now := time.Now()

		cairoSurface.Save()
		left.Draw(cairoSurface, image.Rect(50, 50, 550, 550))
		cairoSurface.Restore()

		cairoSurface.Save()
		right.Draw(cairoSurface, image.Rect(600, 50, 1100, 550))
		cairoSurface.Restore()

		cairoSurface.Save()
		final.Draw(cairoSurface, image.Rect(1100, 50, 1150, 550))
		cairoSurface.Restore()

		// Finally draw to the screen
		cairoSurface.Flush()
		window.UpdateSurface()

		if bench {
			fmt.Printf("                              \r")
			fmt.Printf("%v\r", time.Since(now))
		}
	}
}
