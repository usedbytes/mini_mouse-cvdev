package main

import (
	"encoding/gob"
	"fmt"
	"flag"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	_ "image/gif"
	_ "image/jpeg"
	"math"
	"os"
	"log"
	_ "path/filepath"
	"runtime/pprof"
	"time"

	"github.com/ungerik/go-cairo"
	"github.com/veandco/go-sdl2/sdl"

	"github.com/usedbytes/mini_mouse/ui/widget"
	"github.com/usedbytes/mini_mouse/cv"
)

var bench bool
var profile bool
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

		defaultProfile = false
		usageProfile   = "Serve CPU profile at :6060"
	)

	flag.BoolVar(&bench, "b", defaultBench, usageBench)
	flag.BoolVar(&profile, "p", defaultProfile, usageProfile)
	flag.BoolVar(&ycbcr, "y", defaultYCbCr, usageYCbCr)
}

func writeImage(img image.Image, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}

	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return nil
}

func roundUp(a, to int) int {
	return int((uint32(a) + uint32(to - 1)) & ^uint32(to - 1))
}

func runAlgorithm(in, out image.Image) image.Image {
	// Find and amplify edges
	diff := cv.DeltaCByCol(in)
	minMax := cv.MinMaxRowwise(diff)
	cv.ExpandContrastRowWise(diff, minMax)
	cv.Threshold(diff, 128)

	// Attempt to ignore noisy rows (likely above/below the target)
	w, h := cv.ImageDims(diff)
	for y := 0; y < h; y++ {
		row := diff.Pix[y * diff.Stride : y * diff.Stride + w]
		blobs := cv.FindBlobs(row)
		if len(blobs) != 2 {
			for x := 0; x < len(row); x++ {
				row[x] = 0
			}
		}
	}

	// Find vertical lines in the non-noisy bits
	summed := cv.FindVerticalLines(diff)
	minMax = cv.MinMaxRowwise(summed)
	cv.ExpandContrastRowWise(summed, minMax)
	cv.Threshold(summed, 128)
	fmt.Println(summed.Pix)

	// Hopefully we're left with exactly two blobs, marking the edges
	// TODO: Should handle 1 (one edge only) and 0 (full FoV filled) blob too
	blobs := cv.FindBlobs(summed.Pix)
	scale := in.Bounds().Dx() / len(summed.Pix)

	fmt.Println("Blobs", blobs)

	var target cv.Tuple
	if len(blobs) == 2 {
		target = cv.Tuple{
			roundUp((blobs[0].First + blobs[0].Second) * scale / 2, 2),
			roundUp((blobs[1].First + blobs[1].Second) * scale / 2, 2),
		}
	}

	fmt.Println(target)

	targetColor := in.(*image.YCbCr).YCbCrAt((target.First + target.Second) / 2, in.Bounds().Dy() / 2)
	fmt.Println(targetColor)

	//horz := cv.FindHorizonROI(in, image.Rect(target.First, 0, target.Second, in.Bounds().Dy()))
	horz := float32(math.NaN())
	roi := image.Rect(target.First, 0, target.Second, in.Bounds().Dy())
	{
		diff := cv.DeltaCByRowROI(in, roi)
		minMax := cv.MinMaxColwise(diff)
		cv.ExpandContrastColWise(diff, minMax)
		cv.Threshold(diff, 128)

		summed := cv.FindHorizontalLines(diff)
		minMax = cv.MinMaxColwise(summed)
		cv.ExpandContrastColWise(summed, minMax)
		cv.Threshold(summed, 128)


		blobs := cv.FindBlobs(summed.Pix)
		scale := roi.Dy() / len(summed.Pix)

		if len(blobs) == 0 {
			horz = float32(math.NaN())
			goto eh
		}

		avgs := make([]uint8, 0, len(blobs))
		for _, b := range blobs {
			avgs = append(avgs, cv.AverageDeltaCROIConst(in, b.First * scale, targetColor, roi))
		}

		fmt.Println("Avgs:", avgs)

		min := uint8(255)
		minIdx := -1
		for i, m := range avgs {
			if m < min {
				min = m
				minIdx = i
			}
		}

		b := blobs[minIdx]
		fmt.Println("Blob", minIdx, "at", b)
		horz = float32((b.First + b.Second + 1) / 2) / float32(len(summed.Pix))
	}
eh:

	if !profile {
		bottom := out.Bounds().Dy()

		if float64(horz) != math.NaN() {
			bottom = int(horz * float32(out.Bounds().Dy()))
		}

		red := &image.Uniform{color.RGBA{0x80, 0, 0, 0x80}}

		rect := image.Rect(target.First, 0, target.Second, bottom)
		draw.Draw(out.(draw.Image), rect, red, image.ZP, draw.Over)
	}

	//summed := cv.FindVerticalLines(diff)
	//minMax = cv.MinMaxRowwise(summed)
	//cv.ExpandContrastRowWise(summed, minMax)
	//cv.Threshold(summed, 150)

	return nil
}

func updateImage(fname string) {
	fmt.Println(fname)
	inFile, err := os.Open(fname)
	if err != nil {
		panic(err)
	}

	img, _, err := image.Decode(inFile)
	if err != nil {
		panic(err)
	}

	if ycbcr {
		w, h := img.Bounds().Dx(), int(float64(img.Bounds().Dy()) * 1.0)

		h = h - (h % 2)
		ycbcrImg := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)

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

		//base := fname[:len(fname) - len(filepath.Ext(fname))]
		//outfile := fmt.Sprintf("%s-rgb.png", base)
		//writeImage(img, outfile)
	}


	var mod image.Image = image.NewRGBA(img.Bounds())
	draw.Draw(mod.(draw.Image), img.Bounds(), img, image.ZP, draw.Src)

	if profile {
		f, err := os.Create("cpu.prof")
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}

		start := time.Now()
		for {
			_ = cv.RunAlgorithm(img, mod, profile)
			if time.Since(start) >= 5 * time.Second {
				break
			}
		}

		pprof.StopCPUProfile()
		f.Close()
		fmt.Println("Profile done")
	} else {
		start := time.Now()
		ret := cv.RunAlgorithm(img, mod, profile)
		fmt.Println(time.Since(start))

		if ret != nil {
			mod = ret
		}
	}

	left.SetImage(img)
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

	grad := cairo.NewPatternLinear(cairo.Linear{0, 0, float64(windowW) / 2, float64(windowH) / 2})
	grad.SetExtend(cairo.EXTEND_REFLECT)
	grad.AddColorStopRGB(0, 0, 1.0, 0)
	grad.AddColorStopRGB(1.0, 0, 0, 1.0)
	cairoSurface.SetSource(grad)
	grad.Destroy()
	cairoSurface.Rectangle(0, 0, float64(windowW), float64(windowH))
	cairoSurface.Fill()

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
		final.Draw(cairoSurface, image.Rect(1101, 50, 1150, 550))
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
