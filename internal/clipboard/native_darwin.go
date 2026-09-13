//go:build darwin && cgo

package clipboard

/*
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices
#include <stdlib.h>

typedef struct rtl_clipboard rtl_clipboard;
int rtl_clipboard_create(rtl_clipboard **out, char **error_text);
int rtl_clipboard_create_named(const char *name, rtl_clipboard **out, char **error_text);
void rtl_clipboard_destroy(rtl_clipboard *clipboard);
int rtl_clipboard_snapshot(rtl_clipboard *clipboard, long long *change_count, char **text, int *text_length, int *has_text, char **error_text);
int rtl_clipboard_text_if_current(rtl_clipboard *clipboard, long long expected_change_count, char **text, int *text_length, int *has_text, int *current, char **error_text);
int rtl_clipboard_foreground(rtl_clipboard *clipboard, int *foreground, char **error_text);
int rtl_clipboard_replace_if_current(rtl_clipboard *clipboard, long long expected_change_count, const char *text, int text_length, long long *new_change_count, int *replaced, char **error_text);
int rtl_clipboard_test_external_copy(rtl_clipboard *clipboard, const char *text, int text_length);
int rtl_clipboard_test_external_file(rtl_clipboard *clipboard);
int rtl_clipboard_test_text(rtl_clipboard *clipboard, char **text, int *text_length);
*/
import "C"

import (
	"errors"
	"unsafe"
)

type nativeSource struct{ ptr *C.rtl_clipboard }

func newNativeSource() (*nativeSource, error) {
	var ptr *C.rtl_clipboard
	var errorText *C.char
	if err := nativeError(C.rtl_clipboard_create(&ptr, &errorText), errorText); err != nil {
		return nil, err
	}
	return &nativeSource{ptr: ptr}, nil
}

func newNamedNativeSource(name string) (*nativeSource, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	var ptr *C.rtl_clipboard
	var errorText *C.char
	if err := nativeError(C.rtl_clipboard_create_named(cname, &ptr, &errorText), errorText); err != nil {
		return nil, err
	}
	return &nativeSource{ptr: ptr}, nil
}

func (n *nativeSource) testExternalCopy(text string) error {
	data := C.CBytes([]byte(text))
	defer C.free(data)
	if C.rtl_clipboard_test_external_copy(n.ptr, (*C.char)(data), C.int(len(text))) != 0 {
		return errors.New("clipboard: named pasteboard external copy failed")
	}
	return nil
}

func (n *nativeSource) testExternalFile() error {
	if C.rtl_clipboard_test_external_file(n.ptr) != 0 {
		return errors.New("clipboard: named pasteboard external file copy failed")
	}
	return nil
}

func (n *nativeSource) testText() (string, error) {
	var text *C.char
	var length C.int
	if C.rtl_clipboard_test_text(n.ptr, &text, &length) != 0 {
		return "", errors.New("clipboard: named pasteboard text read failed")
	}
	defer C.free(unsafe.Pointer(text))
	return string(C.GoBytes(unsafe.Pointer(text), length)), nil
}

func (n *nativeSource) snapshot() (clipboardSample, error) {
	var change C.longlong
	var text *C.char
	var length C.int
	var hasText C.int
	var errorText *C.char
	if err := nativeError(C.rtl_clipboard_snapshot(n.ptr, &change, &text, &length, &hasText, &errorText), errorText); err != nil {
		return clipboardSample{}, err
	}
	defer C.free(unsafe.Pointer(text))
	sample := clipboardSample{changeCount: int64(change), hasText: hasText != 0}
	if sample.hasText && length > 0 {
		sample.text = string(C.GoBytes(unsafe.Pointer(text), length))
	}
	return sample, nil
}

func (n *nativeSource) foreground() (bool, error) {
	var foreground C.int
	var errorText *C.char
	if err := nativeError(C.rtl_clipboard_foreground(n.ptr, &foreground, &errorText), errorText); err != nil {
		return false, err
	}
	return foreground != 0, nil
}

func (n *nativeSource) textIfCurrent(changeCount int64) (clipboardSample, bool, error) {
	var text *C.char
	var length C.int
	var hasText C.int
	var current C.int
	var errorText *C.char
	if err := nativeError(C.rtl_clipboard_text_if_current(n.ptr, C.longlong(changeCount), &text, &length, &hasText, &current, &errorText), errorText); err != nil {
		return clipboardSample{}, false, err
	}
	defer C.free(unsafe.Pointer(text))
	sample := clipboardSample{changeCount: changeCount, hasText: hasText != 0}
	if sample.hasText && length > 0 {
		sample.text = string(C.GoBytes(unsafe.Pointer(text), length))
	}
	return sample, current != 0, nil
}

func (n *nativeSource) replaceIfCurrent(changeCount int64, text string) (int64, bool, error) {
	if len(text) > maxTextBytes {
		return 0, false, errors.New("clipboard: replacement exceeds 1 MiB")
	}
	var data unsafe.Pointer
	if len(text) > 0 {
		data = C.CBytes([]byte(text))
		defer C.free(data)
	}
	var newChange C.longlong
	var replaced C.int
	var errorText *C.char
	result := C.rtl_clipboard_replace_if_current(n.ptr, C.longlong(changeCount), (*C.char)(data), C.int(len(text)), &newChange, &replaced, &errorText)
	if err := nativeError(result, errorText); err != nil {
		return 0, false, err
	}
	return int64(newChange), replaced != 0, nil
}

func (n *nativeSource) close() {
	if n.ptr != nil {
		C.rtl_clipboard_destroy(n.ptr)
		n.ptr = nil
	}
}

func nativeError(result C.int, text *C.char) error {
	if text != nil {
		defer C.free(unsafe.Pointer(text))
	}
	if result == 0 {
		return nil
	}
	if text != nil {
		return errors.New(C.GoString(text))
	}
	return errors.New("clipboard: AppKit operation failed")
}
