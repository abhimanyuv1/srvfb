// Copyright 2018 Axel Wagner
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package fb implements framebuffer interaction via ioctls and mmap.
package fb

import (
	"errors"
	"fmt"
	"image"
	"unsafe"

	"golang.org/x/sys/unix"
)

type Device struct {
	fd    uintptr
	mmap  []byte
	finfo FixScreeninfo
}

func Open(dev string) (*Device, error) {
	fd, err := unix.Open(dev, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %v", dev, err)
	}
	if int(uintptr(fd)) != fd {
		unix.Close(fd)
		return nil, errors.New("fd overflows")
	}
	d := &Device{fd: uintptr(fd)}

	_, _, eno := unix.Syscall(unix.SYS_IOCTL, d.fd, FBIOGET_FSCREENINFO, uintptr(unsafe.Pointer(&d.finfo)))
	if eno != 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("FBIOGET_FSCREENINFO: %v", eno)
	}

	d.mmap, err = unix.Mmap(fd, 0, int(d.finfo.Smem_len), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("mmap: %v", err)
	}
	return d, nil
}

func (d *Device) VarScreeninfo() (VarScreeninfo, error) {
	var vinfo VarScreeninfo
	_, _, eno := unix.Syscall(unix.SYS_IOCTL, d.fd, FBIOGET_VSCREENINFO, uintptr(unsafe.Pointer(&vinfo)))
	if eno != 0 {
		return vinfo, fmt.Errorf("FBIOGET_VSCREENINFO: %v", eno)
	}
	return vinfo, nil
}

func (d *Device) Image() (image.Image, error) {
	vinfo, err := d.VarScreeninfo()
	if err != nil {
		return nil, err
	}
	if vinfo.Bits_per_pixel != 16 {
		return nil, fmt.Errorf("%d bits per pixel unsupported", vinfo.Bits_per_pixel)
	}
	virtual := image.Rect(0, 0, int(vinfo.Xres_virtual), int(vinfo.Yres_virtual))
	if virtual.Dx()*virtual.Dy()*2 != len(d.mmap) {
		return nil, errors.New("virtual resolution doesn't match framebuffer size")
	}
	visual := image.Rect(int(vinfo.Xoffset), int(vinfo.Yoffset), int(vinfo.Xres), int(vinfo.Yres))
	if !visual.In(virtual) {
		return nil, errors.New("visual resolution not contained in virtual resolution")
	}
	return &image.Gray16{
		Pix:    d.mmap,
		Stride: int(d.finfo.Line_length),
		Rect:   visual,
	}, nil
}

// RGBA decodes the visible part of the framebuffer into im, using the color
// bitfields reported by the device. im is reallocated if its size doesn't
// match the visible resolution.
func (d *Device) RGBA(im *image.RGBA) error {
	vinfo, err := d.VarScreeninfo()
	if err != nil {
		return err
	}
	return decodeRGBA(im, d.mmap, int(d.finfo.Line_length), vinfo)
}

func decodeRGBA(im *image.RGBA, mem []byte, stride int, vinfo VarScreeninfo) error {
	bpp := vinfo.Bits_per_pixel
	if bpp != 16 && bpp != 24 && bpp != 32 {
		return fmt.Errorf("%d bits per pixel unsupported", bpp)
	}
	var (
		lut   [3][256]uint8
		shift [3]uint32
		mask  [3]uint32
	)
	for i, bf := range []Bitfield{vinfo.Red, vinfo.Green, vinfo.Blue} {
		if bf.Length == 0 || bf.Right != 0 || bf.Offset+bf.Length > bpp {
			return fmt.Errorf("unsupported color bitfield %+v", bf)
		}
		n, sh := bf.Length, bf.Offset
		if n > 8 {
			sh += n - 8
			n = 8
		}
		max := uint32(1)<<n - 1
		for v := uint32(0); v <= max; v++ {
			lut[i][v] = uint8(v * 255 / max)
		}
		shift[i], mask[i] = sh, max
	}

	w, h := int(vinfo.Xres), int(vinfo.Yres)
	x0, y0 := int(vinfo.Xoffset), int(vinfo.Yoffset)
	bytespp := int(bpp / 8)
	if w == 0 || h == 0 || (x0+w)*bytespp > stride || (y0+h)*stride > len(mem) {
		return errors.New("visual resolution not contained in framebuffer")
	}
	if r := image.Rect(0, 0, w, h); im.Rect != r {
		*im = *image.NewRGBA(r)
	}
	for y := 0; y < h; y++ {
		src := mem[(y0+y)*stride+x0*bytespp:][:w*bytespp]
		dst := im.Pix[y*im.Stride:][:4*w]
		for x := 0; x < w; x++ {
			var v uint32
			for i := bytespp - 1; i >= 0; i-- {
				v = v<<8 | uint32(src[x*bytespp+i])
			}
			dst[4*x] = lut[0][v>>shift[0]&mask[0]]
			dst[4*x+1] = lut[1][v>>shift[1]&mask[1]]
			dst[4*x+2] = lut[2][v>>shift[2]&mask[2]]
			dst[4*x+3] = 0xff
		}
	}
	return nil
}

func (d *Device) Close() error {
	e1 := unix.Munmap(d.mmap)
	if e2 := unix.Close(int(d.fd)); e2 != nil {
		return e2
	}
	return e1
}
