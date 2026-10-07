package fb

import (
	"image"
	"image/color"
	"testing"
)

// The decoder must honor the bitfields the device reports rather than assume a
// channel order, and must skip line padding and the pan offset.
func TestDecodeRGBA(t *testing.T) {
	tcs := []struct {
		name  string
		vinfo VarScreeninfo
		// one pixel, little endian
		px   []byte
		want color.RGBA
	}{
		{
			name: "rgb565",
			vinfo: VarScreeninfo{
				Bits_per_pixel: 16,
				Red:            Bitfield{Offset: 11, Length: 5},
				Green:          Bitfield{Offset: 5, Length: 6},
				Blue:           Bitfield{Offset: 0, Length: 5},
			},
			px:   []byte{0x1f, 0xf8},
			want: color.RGBA{0xff, 0, 0xff, 0xff},
		},
		{
			name: "bgr888",
			vinfo: VarScreeninfo{
				Bits_per_pixel: 24,
				Red:            Bitfield{Offset: 16, Length: 8},
				Green:          Bitfield{Offset: 8, Length: 8},
				Blue:           Bitfield{Offset: 0, Length: 8},
			},
			px:   []byte{0x30, 0x20, 0x10},
			want: color.RGBA{0x10, 0x20, 0x30, 0xff},
		},
		{
			name: "rgbx8888",
			vinfo: VarScreeninfo{
				Bits_per_pixel: 32,
				Red:            Bitfield{Offset: 0, Length: 8},
				Green:          Bitfield{Offset: 8, Length: 8},
				Blue:           Bitfield{Offset: 16, Length: 8},
			},
			px:   []byte{0x10, 0x20, 0x30, 0x00},
			want: color.RGBA{0x10, 0x20, 0x30, 0xff},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			const w, h, xoff, yoff, pad = 2, 2, 1, 1, 3
			bytespp := len(tc.px)
			stride := (xoff+w)*bytespp + pad
			mem := make([]byte, stride*(yoff+h))
			// Only the bottom right visible pixel is set.
			copy(mem[(yoff+1)*stride+(xoff+1)*bytespp:], tc.px)

			vinfo := tc.vinfo
			vinfo.Xres, vinfo.Yres = w, h
			vinfo.Xoffset, vinfo.Yoffset = xoff, yoff

			im := new(image.RGBA)
			if err := decodeRGBA(im, mem, stride, vinfo); err != nil {
				t.Fatal(err)
			}
			if im.Rect != image.Rect(0, 0, w, h) {
				t.Fatalf("Rect = %v", im.Rect)
			}
			black := color.RGBA{0, 0, 0, 0xff}
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					want := black
					if x == 1 && y == 1 {
						want = tc.want
					}
					if got := im.RGBAAt(x, y); got != want {
						t.Errorf("At(%d, %d) = %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestDecodeRGBAErrors(t *testing.T) {
	rgb565 := VarScreeninfo{
		Xres: 2, Yres: 2,
		Bits_per_pixel: 16,
		Red:            Bitfield{Offset: 11, Length: 5},
		Green:          Bitfield{Offset: 5, Length: 6},
		Blue:           Bitfield{Offset: 0, Length: 5},
	}
	mem := make([]byte, 8)

	bpp := rgb565
	bpp.Bits_per_pixel = 8
	noRed := rgb565
	noRed.Red.Length = 0
	tooTall := rgb565
	tooTall.Yres = 3

	for name, vinfo := range map[string]VarScreeninfo{"8bpp": bpp, "no red channel": noRed, "out of bounds": tooTall} {
		if err := decodeRGBA(new(image.RGBA), mem, 4, vinfo); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := decodeRGBA(new(image.RGBA), mem, 4, rgb565); err != nil {
		t.Errorf("valid: %v", err)
	}
}
