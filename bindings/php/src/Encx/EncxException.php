<?php

declare(strict_types=1);

namespace Encx;

/**
 * Thrown when a call into the encx shared library fails.
 *
 * The message is the error text produced by the Go side, so it reads the same
 * as the error a Go caller of encx would get.
 */
class EncxException extends \RuntimeException
{
}
