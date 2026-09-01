<?php

declare(strict_types=1);

namespace Encx;

/**
 * Loads the encx C-shared library and performs the calls behind the generated
 * bindings.
 *
 * Every exported function returns a malloc'd C string holding a JSON envelope:
 * `{"ok":true,"value":...}`, `{"ok":true}` or `{"ok":false,"error":"..."}`.
 * This class unwraps that envelope, frees the string, and turns a failed
 * envelope into an {@see EncxException}.
 */
final class Ffi
{
    private static ?\FFI $ffi = null;

    private static ?string $libraryPath = null;

    private static ?string $headerPath = null;

    /**
     * Overrides the shared library location. Must be called before the first
     * binding call; it resets any already loaded library.
     */
    public static function setLibraryPath(string $path): void
    {
        self::$libraryPath = $path;
        self::$ffi = null;
    }

    /**
     * Overrides the C header location. Must be called before the first binding
     * call; it resets any already loaded library.
     */
    public static function setHeaderPath(string $path): void
    {
        self::$headerPath = $path;
        self::$ffi = null;
    }

    /**
     * Returns the platform-specific default file name of the shared library.
     */
    public static function defaultLibraryName(): string
    {
        return match (PHP_OS_FAMILY) {
            'Darwin' => 'libencx.dylib',
            'Windows' => 'encx.dll',
            default => 'libencx.so',
        };
    }

    /**
     * Resolves the shared library path from the explicit override, the
     * ENCX_LIBRARY environment variable, or the default build output directory.
     */
    public static function libraryPath(): string
    {
        if (self::$libraryPath !== null) {
            return self::$libraryPath;
        }

        $fromEnv = getenv('ENCX_LIBRARY');
        if (is_string($fromEnv) && $fromEnv !== '') {
            return $fromEnv;
        }

        return dirname(__DIR__, 2) . '/lib/' . self::defaultLibraryName();
    }

    /**
     * Resolves the C header path from the explicit override, the ENCX_HEADER
     * environment variable, or the generated header next to the bindings.
     */
    public static function headerPath(): string
    {
        if (self::$headerPath !== null) {
            return self::$headerPath;
        }

        $fromEnv = getenv('ENCX_HEADER');
        if (is_string($fromEnv) && $fromEnv !== '') {
            return $fromEnv;
        }

        return dirname(__DIR__, 2) . '/encx.h';
    }

    /**
     * Returns the loaded FFI handle, loading the library on first use.
     *
     * @throws EncxException when FFI is unavailable or the library cannot be loaded
     */
    public static function instance(): \FFI
    {
        if (self::$ffi !== null) {
            return self::$ffi;
        }

        if (!extension_loaded('ffi')) {
            throw new EncxException(
                'the PHP FFI extension is required by the encx bindings but is not loaded'
            );
        }

        $header = self::headerPath();
        if (!is_file($header)) {
            throw new EncxException(sprintf('encx C header not found at %s', $header));
        }

        $library = self::libraryPath();
        if (!is_file($library)) {
            throw new EncxException(sprintf(
                'encx shared library not found at %s; build it with bindings/php/build.sh or set ENCX_LIBRARY',
                $library
            ));
        }

        $source = file_get_contents($header);
        if ($source === false) {
            throw new EncxException(sprintf('cannot read encx C header at %s', $header));
        }

        try {
            self::$ffi = \FFI::cdef($source, $library);
        } catch (\FFI\Exception $e) {
            throw new EncxException(
                sprintf('cannot load encx shared library %s: %s', $library, $e->getMessage()),
                0,
                $e
            );
        }

        return self::$ffi;
    }

    /**
     * Calls an exported function and returns the decoded `value` field, or null
     * when the envelope carries no value.
     *
     * @param list<mixed> $args
     *
     * @throws EncxException when the call fails on the Go side or the envelope is malformed
     */
    public static function call(string $function, array $args = []): mixed
    {
        $ffi = self::instance();

        /** @var \FFI\CData|null $pointer */
        $pointer = $ffi->$function(...$args);
        if ($pointer === null) {
            throw new EncxException(sprintf('%s returned a null envelope', $function));
        }

        try {
            $json = \FFI::string($pointer);
        } finally {
            // The Go side allocated this string with C.CString; it is ours to free
            // whether or not decoding the envelope succeeds.
            $ffi->encx_string_free($pointer);
        }

        try {
            $envelope = json_decode($json, true, 512, JSON_THROW_ON_ERROR);
        } catch (\JsonException $e) {
            throw new EncxException(
                sprintf('%s returned a malformed envelope: %s', $function, $e->getMessage()),
                0,
                $e
            );
        }

        if (!is_array($envelope) || !array_key_exists('ok', $envelope)) {
            throw new EncxException(sprintf('%s returned an envelope without an "ok" field', $function));
        }

        if ($envelope['ok'] !== true) {
            $error = $envelope['error'] ?? null;
            throw new EncxException(is_string($error) && $error !== ''
                ? $error
                : sprintf('%s failed without an error message', $function));
        }

        return $envelope['value'] ?? null;
    }

    /**
     * Decodes a `[]byte` result, which the Go side encodes as base64.
     *
     * @throws EncxException when the payload is not valid base64
     */
    public static function decodeBytes(mixed $value): string
    {
        if ($value === null) {
            return '';
        }

        if (!is_string($value)) {
            throw new EncxException('expected a base64 string for a bytes result');
        }

        $decoded = base64_decode($value, true);
        if ($decoded === false) {
            throw new EncxException('bytes result is not valid base64');
        }

        return $decoded;
    }
}
