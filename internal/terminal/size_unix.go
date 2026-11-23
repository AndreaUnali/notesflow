//go:build unix

package terminal

import "golang.org/x/sys/unix"

func cellRatio(fd uintptr, cols, rows int) float64 {
	size, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	if err == nil && size.Xpixel > 0 && size.Ypixel > 0 && cols > 0 && rows > 0 {
		return max(.3, min(.8, float64(size.Xpixel)*float64(rows)/(float64(size.Ypixel)*float64(cols))))
	}
	return .5
}

func cellPixels(fd uintptr) (int, int) {
	size, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	if err != nil || size.Col == 0 || size.Row == 0 {
		return 0, 0
	}
	return int(size.Xpixel) / int(size.Col), int(size.Ypixel) / int(size.Row)
}
