package main

import (
	"encoding/gob"
	"fmt"
	"flag"
	"image"
	"image/draw"
	"image/png"
	_ "image/gif"
	_ "image/jpeg"
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

func runAlgorithm(in, out image.Image) image.Image {
	horz := cv.FindHorizon(in)
	fmt.Println(horz)

	diff := cv.DeltaCByCol(in)
	minMax := cv.MinMaxRowwise(diff)
	cv.ExpandContrastRowWise(diff, minMax)
	cv.Threshold(diff, 128)

	for y := 0; y < diff.Bounds().Dy(); y++ {
		blobs := cv.FindBlobs(diff.Pix[y * diff.Stride:y * diff.Stride + diff.Bounds().Dx()])
		fmt.Println(y, ":", len(blobs))
	}

	//summed := cv.FindVerticalLines(diff)
	//minMax = cv.MinMaxRowwise(summed)
	//cv.ExpandContrastRowWise(summed, minMax)
	//cv.Threshold(summed, 150)

	return diff
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
			_ = runAlgorithm(img, mod)
			if time.Since(start) >= 5 * time.Second {
				break
			}
		}

		pprof.StopCPUProfile()
		f.Close()
		fmt.Println("Profile done")
	} else {
		start := time.Now()
		ret := runAlgorithm(img, mod)
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
