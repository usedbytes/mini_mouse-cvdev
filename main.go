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
	"os"
	"time"

	"github.com/ungerik/go-cairo"
	"github.com/veandco/go-sdl2/sdl"

	"github.com/usedbytes/mini_mouse/ui/widget"
	"github.com/usedbytes/mini_mouse/cv"
)

var bench bool
var ycbcr bool
var left *widget.ImageWidget
var right *widget.ImageWidget
var final *widget.ImageWidget

func init() {
	gob.Register(&image.NRGBA{})
	gob.Register(&image.Gray{})

	const (
		defaultBench = false
		usageBench   = "Measure drawing time"

		defaultYCbCr = false
		usageYCbCr   = "Treat RGB data as YCbCr"
	)

	flag.BoolVar(&bench, "b", defaultBench, usageBench)
	flag.BoolVar(&ycbcr, "y", defaultYCbCr, usageYCbCr)
}

func diffImage(in image.Image) *image.Gray {
	w, h := in.Bounds().Dx(), in.Bounds().Dy()
	var out *image.Gray

	switch v := in.(type) {
	case *image.YCbCr:
		hsub, vsub := 1, 1
		switch v.SubsampleRatio {
		case image.YCbCrSubsampleRatio422:
			hsub, vsub = 2, 1
		case image.YCbCrSubsampleRatio420:
			hsub, vsub = 2, 2
		}
		cols, rows := w / hsub, h / vsub
		out = image.NewGray(image.Rect(0, 0, cols, rows - 1))
		for x := 0; x < w; x += hsub {
			for y := 0; y < h - 1; y += vsub {
				s, d := v.YCbCrAt(x, y), v.YCbCrAt(x, y + vsub)
				diff := color.Gray{cv.DeltaC(s, d)}
				out.SetGray(x / hsub, y / vsub, diff)
			}
		}
	default:
		_ = v
		out = image.NewGray(image.Rect(0, 0, w, h - 1))
		for x := 0; x < w; x++ {
			for y := 0; y < h - 1; y++ {
				diff := color.Gray{cv.DeltaC(in.At(x, y), in.At(x, y + 1))}
				out.SetGray(x, y, diff)
			}
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

	sums := cv.SumLines(img)

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

	if ycbcr {
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		ycbcrImg := image.NewYCbCr(img.Bounds(), image.YCbCrSubsampleRatio420)

		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				yoff := ycbcrImg.YOffset(x, y)
				coff := ycbcrImg.COffset(x, y)

				ycbcrImg.Y[yoff] = byte(r >> 8)
				ycbcrImg.Cb[coff] = byte(g >> 8)
				ycbcrImg.Cr[coff] = byte(b >> 8)
			}
		}

		img = ycbcrImg
	}

	left.SetImage(img)

	grad := diffImage(img)
	minMax := cv.MinMaxColwise(grad)
	cv.ExpandContrastColWise(grad, minMax)
	cv.Threshold(grad, 128)
	right.SetImage(grad)

	summed := findLines(grad)
	final.SetImage(summed)

	minMax = cv.MinMaxColwise(summed)
	cv.ExpandContrastColWise(summed, minMax)
	cv.Threshold(summed, 128)

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
