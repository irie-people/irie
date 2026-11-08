package sdk

import "golang.org/x/sys/unix"

// rawMode puts the terminal fd in raw mode: keys come as they are typed,
// unechoed, and Ctrl+C is a key rather than a signal. Output is processed as
// before. It returns how to put the terminal back.
func rawMode(fd int) (restore func(), err error) {
	old, err := unix.IoctlGetTermios(fd, getTermios)
	if err != nil {
		return nil, err
	}
	t := *old
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, setTermios, &t); err != nil {
		return nil, err
	}
	return func() { unix.IoctlSetTermios(fd, setTermios, old) }, nil
}

// columns is the terminal fd's width, or 0 when fd is not a terminal.
func columns(fd int) int {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(ws.Col)
}
