package vm

import "syscall"

// errnoNumbers maps each registered Errno::Exxx name to its platform errno
// number, taken from the host's syscall table so Errno::ENOENT::Errno matches the
// value the host MRI reports (these differ across platforms — EAGAIN is 11 on
// Linux, 35 on the BSDs/macOS). SystemCallError#errno reads the value back off
// the class's Errno constant.
var errnoNumbers = map[string]int64{
	"ENOENT":       int64(syscall.ENOENT),
	"EEXIST":       int64(syscall.EEXIST),
	"EACCES":       int64(syscall.EACCES),
	"ENOTDIR":      int64(syscall.ENOTDIR),
	"EISDIR":       int64(syscall.EISDIR),
	"EPERM":        int64(syscall.EPERM),
	"EINVAL":       int64(syscall.EINVAL),
	"EAGAIN":       int64(syscall.EAGAIN),
	"EBADF":        int64(syscall.EBADF),
	"ESRCH":        int64(syscall.ESRCH),
	"EIO":          int64(syscall.EIO),
	"ENOSPC":       int64(syscall.ENOSPC),
	"EROFS":        int64(syscall.EROFS),
	"ENXIO":        int64(syscall.ENXIO),
	"ENOTEMPTY":    int64(syscall.ENOTEMPTY),
	"ECONNREFUSED": int64(syscall.ECONNREFUSED),
	"ECONNRESET":   int64(syscall.ECONNRESET),
	"ETIMEDOUT":    int64(syscall.ETIMEDOUT),
	"EPIPE":        int64(syscall.EPIPE),
	"ELOOP":        int64(syscall.ELOOP),
	"ENAMETOOLONG": int64(syscall.ENAMETOOLONG),
	"EADDRINUSE":   int64(syscall.EADDRINUSE),
	"EINTR":        int64(syscall.EINTR),
	"ECHILD":       int64(syscall.ECHILD),
	"ENOMEM":       int64(syscall.ENOMEM),
	"EXDEV":        int64(syscall.EXDEV),
	"EMFILE":       int64(syscall.EMFILE),
	"ENFILE":       int64(syscall.ENFILE),
}
