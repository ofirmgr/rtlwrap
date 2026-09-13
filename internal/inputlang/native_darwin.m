#import <Carbon/Carbon.h>
#import <stdlib.h>
#import <pthread.h>

int rtl_input_language_main_thread(void) {
    return pthread_main_np();
}

void rtl_input_language_process_events(void) {
    // TIS caches the selected source until distributed notifications are
    // delivered on the main run loop. Polling alone keeps the startup source.
    CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.05, false);
}

char *rtl_current_input_language(void) {
    TISInputSourceRef source = TISCopyCurrentKeyboardInputSource();
    if (source == NULL) return NULL;

    char *result = NULL;
    CFArrayRef languages = TISGetInputSourceProperty(
        source, kTISPropertyInputSourceLanguages);
    if (languages != NULL && CFGetTypeID(languages) == CFArrayGetTypeID() &&
        CFArrayGetCount(languages) > 0) {
        CFTypeRef language = CFArrayGetValueAtIndex(languages, 0);
        if (language != NULL && CFGetTypeID(language) == CFStringGetTypeID()) {
            CFIndex length = CFStringGetLength((CFStringRef)language);
            CFIndex maxSize = CFStringGetMaximumSizeForEncoding(
                length, kCFStringEncodingUTF8) + 1;
            result = calloc((size_t)maxSize, sizeof(char));
            if (result == NULL || !CFStringGetCString(
                    (CFStringRef)language, result, maxSize,
                    kCFStringEncodingUTF8)) {
                free(result);
                result = NULL;
            }
        }
    }
    CFRelease(source);
    return result;
}
