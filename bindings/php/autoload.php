<?php

declare(strict_types=1);

/**
 * PSR-4 autoloader for the Encx namespace.
 *
 * Composer's autoloader (see composer.json) is equivalent; this file exists so
 * the bindings and their tests can run without Composer installed.
 */

spl_autoload_register(static function (string $class): void {
    $prefix = 'Encx\\';
    if (!str_starts_with($class, $prefix)) {
        return;
    }

    $relative = substr($class, strlen($prefix));
    $file = __DIR__ . '/src/Encx/' . str_replace('\\', '/', $relative) . '.php';
    if (is_file($file)) {
        require $file;
    }
});
