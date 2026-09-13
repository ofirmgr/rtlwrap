#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#import <libproc.h>
#import <stdlib.h>
#import <string.h>
#import <unistd.h>

static const int rtl_max_text_bytes = 1024 * 1024;

typedef struct rtl_clipboard {
    NSPasteboard *pasteboard;
    pid_t host_pid;
    BOOL require_foreground;
} rtl_clipboard;

static void rtl_set_error(char **error_text, const char *message) {
    if (error_text != NULL) {
        *error_text = strdup(message);
    }
}

static BOOL rtl_terminal_bundle(NSString *bundle_id) {
    static NSSet<NSString *> *terminal_bundles;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        terminal_bundles = [[NSSet alloc] initWithArray:@[
            @"com.apple.Terminal",
            @"com.googlecode.iterm2",
            @"dev.warp.Warp-Stable",
            @"dev.warp.Warp",
            @"com.mitchellh.ghostty",
            @"net.kovidgoyal.kitty",
            @"org.alacritty"
        ]];
    });
    return bundle_id != nil && [terminal_bundles containsObject:bundle_id];
}

static BOOL rtl_plain_text_only(NSPasteboard *pasteboard) {
    NSArray<NSPasteboardItem *> *items = pasteboard.pasteboardItems;
    if (items.count > 1) return NO;
    NSArray<NSPasteboardType> *types = items.count == 1 ? items.firstObject.types : pasteboard.types;
    if (types.count == 0) return NO;
    for (NSPasteboardType type in types) {
        if (![type isEqualToString:NSPasteboardTypeString] &&
            ![type isEqualToString:@"public.utf8-plain-text"] &&
            ![type isEqualToString:@"public.utf16-external-plain-text"] &&
            ![type isEqualToString:@"public.text"] &&
            ![type isEqualToString:@"NSStringPboardType"]) {
            return NO;
        }
    }
    return YES;
}

static BOOL rtl_host_is_foreground(pid_t host_pid) {
    // NSRunningApplication.active is runloop-cached. Carbon reads the current
    // front process synchronously, so the polling goroutine has no AppKit loop.
    ProcessSerialNumber front;
    pid_t front_pid = 0;
    return GetFrontProcess(&front) == noErr && GetProcessPID(&front, &front_pid) == noErr && front_pid == host_pid;
}

static pid_t rtl_parent_pid(pid_t pid) {
    struct proc_bsdinfo info;
    int got = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info));
    return got == sizeof(info) ? info.pbi_ppid : 0;
}

static BOOL rtl_find_terminal_ancestor(pid_t *host_pid) {
    pid_t pid = getpid();
    for (int depth = 0; pid > 1 && depth < 128; depth++) {
        NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        if (app != nil && rtl_terminal_bundle(app.bundleIdentifier)) {
            *host_pid = pid;
            return YES;
        }
        pid_t parent = rtl_parent_pid(pid);
        if (parent == pid) break;
        pid = parent;
    }
    return NO;
}

int rtl_clipboard_create(rtl_clipboard **out, char **error_text) {
    @autoreleasepool {
        pid_t host_pid = 0;
        if (!rtl_find_terminal_ancestor(&host_pid)) {
            rtl_set_error(error_text, "clipboard: cannot identify a supported terminal ancestor; watcher disabled");
            return -1;
        }
        rtl_clipboard *clipboard = calloc(1, sizeof(*clipboard));
        if (clipboard == NULL) {
            rtl_set_error(error_text, "clipboard: cannot allocate watcher");
            return -1;
        }
        clipboard->pasteboard = [[NSPasteboard generalPasteboard] retain];
        if (clipboard->pasteboard == nil) {
            free(clipboard);
            rtl_set_error(error_text, "clipboard: system pasteboard is unavailable");
            return -1;
        }
        clipboard->host_pid = host_pid;
        clipboard->require_foreground = YES;
        *out = clipboard;
        return 0;
    }
}

int rtl_clipboard_create_named(const char *name, rtl_clipboard **out, char **error_text) {
    @autoreleasepool {
        NSString *pasteboard_name = [[NSString alloc] initWithUTF8String:name];
        if (pasteboard_name == nil) {
            rtl_set_error(error_text, "clipboard: invalid named pasteboard");
            return -1;
        }
        rtl_clipboard *clipboard = calloc(1, sizeof(*clipboard));
        if (clipboard == NULL) {
            [pasteboard_name release];
            rtl_set_error(error_text, "clipboard: cannot allocate watcher");
            return -1;
        }
        clipboard->pasteboard = [[NSPasteboard pasteboardWithName:pasteboard_name] retain];
        [pasteboard_name release];
        if (clipboard->pasteboard == nil) {
            free(clipboard);
            rtl_set_error(error_text, "clipboard: named pasteboard is unavailable");
            return -1;
        }
        clipboard->require_foreground = NO;
        *out = clipboard;
        return 0;
    }
}

void rtl_clipboard_destroy(rtl_clipboard *clipboard) {
    @autoreleasepool {
        if (!clipboard->require_foreground) {
            [clipboard->pasteboard releaseGlobally];
        }
        [clipboard->pasteboard release];
    }
    free(clipboard);
}

int rtl_clipboard_snapshot(rtl_clipboard *clipboard, long long *change_count, char **text, int *text_length, int *has_text, char **error_text) {
    @autoreleasepool {
        *change_count = clipboard->pasteboard.changeCount;
        *text = NULL; // Content stays unread until the foreground check in Go.
        *text_length = 0;
        *has_text = 0;
        return 0;
    }
}

int rtl_clipboard_text_if_current(rtl_clipboard *clipboard, long long expected_change_count, char **text, int *text_length, int *has_text, int *current, char **error_text) {
    @autoreleasepool {
        *text = NULL;
        *text_length = 0;
        *has_text = 0;
        *current = 0;
        if (clipboard->pasteboard.changeCount != expected_change_count) return 0;
        if (!rtl_plain_text_only(clipboard->pasteboard)) return 0;
        NSString *value = [clipboard->pasteboard stringForType:NSPasteboardTypeString];
        if (value == nil) return 0;
        NSData *data = [value dataUsingEncoding:NSUTF8StringEncoding];
        if (data == nil) {
            rtl_set_error(error_text, "clipboard: cannot read UTF-8 text");
            return -1;
        }
        if (data.length > rtl_max_text_bytes) return 0;
        if (data.length > 0) {
            *text = malloc(data.length);
            if (*text == NULL) {
                rtl_set_error(error_text, "clipboard: cannot allocate clipboard text");
                return -1;
            }
            memcpy(*text, data.bytes, data.length);
        }
        if (clipboard->pasteboard.changeCount != expected_change_count) {
            free(*text);
            *text = NULL;
            return 0;
        }
        *text_length = (int)data.length;
        *has_text = 1;
        *current = 1;
        return 0;
    }
}

int rtl_clipboard_foreground(rtl_clipboard *clipboard, int *foreground, char **error_text) {
    @autoreleasepool {
        if (!clipboard->require_foreground) {
            *foreground = 1;
            return 0;
        }
        NSRunningApplication *host = [NSRunningApplication runningApplicationWithProcessIdentifier:clipboard->host_pid];
        if (host == nil) {
            rtl_set_error(error_text, "clipboard: terminal host is no longer available");
            return -1;
        }
        *foreground = rtl_host_is_foreground(clipboard->host_pid) ? 1 : 0;
        return 0;
    }
}

int rtl_clipboard_replace_if_current(rtl_clipboard *clipboard, long long expected_change_count, const char *text, int text_length, long long *new_change_count, int *replaced, char **error_text) {
    @autoreleasepool {
        *replaced = 0;
        *new_change_count = clipboard->pasteboard.changeCount;
        if (*new_change_count != expected_change_count) return 0;
        if (!rtl_plain_text_only(clipboard->pasteboard)) return 0;
        if (clipboard->require_foreground) {
            NSRunningApplication *host = [NSRunningApplication runningApplicationWithProcessIdentifier:clipboard->host_pid];
            if (host == nil) {
                rtl_set_error(error_text, "clipboard: terminal host is no longer available");
                return -1;
            }
            if (!rtl_host_is_foreground(clipboard->host_pid)) return 0;
        }
        NSString *value = [[NSString alloc] initWithBytes:text length:text_length encoding:NSUTF8StringEncoding];
        if (value == nil) {
            rtl_set_error(error_text, "clipboard: replacement is not valid UTF-8");
            return -1;
        }
        // Content was verified plain-text-only immediately above. Declaring the
        // single UTF-8 representation removes stale alternate text formats.
        if (clipboard->pasteboard.changeCount != expected_change_count ||
            (clipboard->require_foreground && !rtl_host_is_foreground(clipboard->host_pid))) {
            [value release];
            return 0;
        }
        [clipboard->pasteboard clearContents];
        [clipboard->pasteboard declareTypes:@[NSPasteboardTypeString] owner:nil];
        BOOL written = [clipboard->pasteboard setString:value forType:NSPasteboardTypeString];
        [value release];
        if (!written) {
            rtl_set_error(error_text, "clipboard: pasteboard rejected plain text replacement");
            return -1;
        }
        *new_change_count = clipboard->pasteboard.changeCount;
        *replaced = 1;
        return 0;
    }
}

// Test-only callers create a unique named pasteboard. These functions are never
// reached by Start and must not be used with the user's general pasteboard.
int rtl_clipboard_test_external_copy(rtl_clipboard *clipboard, const char *text, int text_length) {
    @autoreleasepool {
        if (clipboard->require_foreground) return -1;
        NSString *value = [[NSString alloc] initWithBytes:text length:text_length encoding:NSUTF8StringEncoding];
        if (value == nil) return -1;
        [clipboard->pasteboard clearContents];
        BOOL written = [clipboard->pasteboard setString:value forType:NSPasteboardTypeString];
        [value release];
        return written ? 0 : -1;
    }
}

int rtl_clipboard_test_external_file(rtl_clipboard *clipboard) {
    @autoreleasepool {
        if (clipboard->require_foreground) return -1;
        [clipboard->pasteboard clearContents];
        return [clipboard->pasteboard setPropertyList:@[@"/tmp/rtlwrap-test-file"] forType:NSFilenamesPboardType] ? 0 : -1;
    }
}

int rtl_clipboard_test_text(rtl_clipboard *clipboard, char **text, int *text_length) {
    @autoreleasepool {
        if (clipboard->require_foreground) return -1;
        *text = NULL;
        *text_length = 0;
        NSString *value = [clipboard->pasteboard stringForType:NSPasteboardTypeString];
        if (value == nil) return -1;
        NSData *data = [value dataUsingEncoding:NSUTF8StringEncoding];
        if (data == nil || data.length > INT_MAX) return -1;
        if (data.length > 0) {
            *text = malloc(data.length);
            if (*text == NULL) return -1;
            memcpy(*text, data.bytes, data.length);
        }
        *text_length = (int)data.length;
        return 0;
    }
}
