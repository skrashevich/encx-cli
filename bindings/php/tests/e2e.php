<?php

declare(strict_types=1);

/**
 * End-to-end test for the PHP bindings, run against cmd/encx-mock.
 *
 * It drives the real shared library through FFI, so it covers the whole chain:
 * PHP -> C ABI -> Go client -> HTTP. Use bindings/php/tests/run-e2e.sh, which
 * builds the library, starts the mock and exports ENCX_MOCK_ADDR.
 */

require __DIR__ . '/../autoload.php';

use Encx\Client;
use Encx\EncxException;
use Encx\Helpers;

/** The game and team the mock serves; see cmd/encx-mock/main.go. */
const MOCK_GAME_ID = 424242;
const MOCK_UNKNOWN_GAME_ID = 999999;
const MOCK_LOGIN = 'tester';
const MOCK_PASSWORD = 'secret';

/**
 * Records the outcome of every named check and decides the exit status.
 */
final class Checks
{
    private int $passed = 0;

    /** @var list<string> */
    private array $failed = [];

    /**
     * Runs one check. A check signals a problem by throwing; everything that
     * escapes is recorded as a failure so a single broken expectation cannot
     * hide the checks that follow it.
     */
    public function run(string $name, callable $body): void
    {
        try {
            $body();
        } catch (\Throwable $e) {
            $this->failed[] = $name;
            printf("FAIL %s: %s\n", $name, $e->getMessage());

            return;
        }

        $this->passed++;
        printf("ok %s\n", $name);
    }

    public function report(): int
    {
        $total = $this->passed + count($this->failed);
        if ($this->failed === []) {
            printf("\n%d/%d checks passed\n", $this->passed, $total);

            return 0;
        }

        printf("\n%d/%d checks passed, failed: %s\n", $this->passed, $total, implode(', ', $this->failed));

        return 1;
    }
}

function fail(string $message): never
{
    throw new RuntimeException($message);
}

function assertTrue(bool $condition, string $message): void
{
    if (!$condition) {
        fail($message);
    }
}

function assertSame(mixed $want, mixed $got, string $what): void
{
    if ($want !== $got) {
        fail(sprintf('%s: want %s, got %s', $what, var_export($want, true), var_export($got, true)));
    }
}

/**
 * Decodes a JSON document a binding returned, failing the check when it is not
 * a JSON object.
 *
 * @return array<string, mixed>
 */
function decodeObject(string $json, string $what): array
{
    assertTrue($json !== '', sprintf('%s returned an empty string', $what));

    try {
        $decoded = json_decode($json, true, 512, JSON_THROW_ON_ERROR);
    } catch (JsonException $e) {
        fail(sprintf('%s did not return decodable JSON: %s', $what, $e->getMessage()));
    }

    if (!is_array($decoded)) {
        fail(sprintf('%s returned %s, want a JSON object', $what, get_debug_type($decoded)));
    }

    return $decoded;
}

$addr = getenv('ENCX_MOCK_ADDR');
if (!is_string($addr) || trim($addr) === '') {
    fwrite(STDERR, "ENCX_MOCK_ADDR must hold the host:port of a running encx-mock; run bindings/php/tests/run-e2e.sh\n");
    exit(1);
}
$addr = trim($addr);

/** The mock speaks plain HTTP, so useHTTP must be on. */
$newClient = static fn (): Client => Client::newClientWithOptions($addr, false, true, 10, 'ru');

$checks = new Checks();
$client = $newClient();

$checks->run('client creation and domain()', static function () use ($client, $addr): void {
    assertSame($addr, $client->domain(), 'domain()');
});

$checks->run('login() returns a successful LoginResponse', static function () use ($client): void {
    $response = $client->login(MOCK_LOGIN, MOCK_PASSWORD);
    $decoded = decodeObject($response, 'login()');
    assertSame(0, $decoded['Error'] ?? null, 'login() Error field');
});

// The handle must keep one live Go client across calls: the session cookies the
// login stored are what make this profile request succeed.
$checks->run('session survives across calls', static function () use ($client): void {
    $profile = decodeObject($client->getProfile(), 'getProfile()');
    assertSame(MOCK_LOGIN, $profile['login'] ?? null, 'getProfile() login field');
});

$checks->run('getGameModel() returns the mock game', static function () use ($client): void {
    $model = decodeObject($client->getGameModel(MOCK_GAME_ID), 'getGameModel()');
    assertSame(MOCK_GAME_ID, $model['GameId'] ?? null, 'GameId');
    $title = $model['GameTitle'] ?? null;
    assertTrue(is_string($title) && $title !== '', sprintf('GameTitle: want a non-empty string, got %s', var_export($title, true)));
});

$checks->run('a Go-side error arrives as EncxException', static function () use ($client): void {
    try {
        $client->getGameModel(MOCK_UNKNOWN_GAME_ID);
    } catch (EncxException $e) {
        assertTrue($e->getMessage() !== '', 'EncxException carries no message');

        return;
    } catch (\Throwable $e) {
        fail(sprintf('want Encx\EncxException, got %s: %s', $e::class, $e->getMessage()));
    }

    fail('getGameModel() on an unknown game returned instead of throwing');
});

$checks->run('harEntryCount() returns an int that tracks traffic', static function () use ($client): void {
    $before = $client->harEntryCount();
    assertTrue(is_int($before), sprintf('harEntryCount() returned %s, want int', get_debug_type($before)));

    $client->setHARRecordingEnabled(true);
    $client->getGameModel(MOCK_GAME_ID);

    $after = $client->harEntryCount();
    assertTrue(is_int($after), sprintf('harEntryCount() returned %s, want int', get_debug_type($after)));
    assertTrue($after > 0, sprintf('harEntryCount() = %d after a recorded request, want > 0', $after));
});

$checks->run('exportHARSnapshot() returns the HARSnapshot fields', static function () use ($client): void {
    $snapshot = $client->exportHARSnapshot();
    assertTrue(is_array($snapshot), sprintf('exportHARSnapshot() returned %s, want array', get_debug_type($snapshot)));

    foreach (['JSON', 'EntryCount'] as $key) {
        assertTrue(array_key_exists($key, $snapshot), sprintf('exportHARSnapshot() has no %s key; keys: %s', $key, implode(', ', array_keys($snapshot))));
    }

    assertTrue(is_string($snapshot['JSON']), sprintf('JSON field is %s, want string', get_debug_type($snapshot['JSON'])));
    assertTrue(is_int($snapshot['EntryCount']), sprintf('EntryCount is %s, want int', get_debug_type($snapshot['EntryCount'])));
    assertTrue($snapshot['EntryCount'] > 0, sprintf('EntryCount = %d, want > 0', $snapshot['EntryCount']));
});

// exportCookies()/importCookies() are the two []byte directions across the C
// ABI: out as a base64 payload, back in as a pointer plus a length.
$checks->run('cookies round-trip through []byte', static function () use ($client, $newClient): void {
    $cookies = $client->exportCookies();
    assertTrue(is_string($cookies), sprintf('exportCookies() returned %s, want string', get_debug_type($cookies)));
    assertTrue($cookies !== '', 'exportCookies() returned an empty payload after a login');

    $recipient = $newClient();
    try {
        $recipient->importCookies($cookies);
    } catch (EncxException $e) {
        fail(sprintf('importCookies() rejected the exported payload: %s', $e->getMessage()));
    } finally {
        $recipient->close();
    }
});

$checks->run('handles are independent', static function () use ($newClient, $addr): void {
    $first = $newClient();
    $second = $newClient();

    $first->login(MOCK_LOGIN, MOCK_PASSWORD);
    assertSame($addr, $first->domain(), 'first client domain()');
    assertSame($addr, $second->domain(), 'second client domain()');

    // Only the first handle holds a session, so the second still looks anonymous.
    $anonymous = decodeObject($second->getProfile(), 'getProfile() on the client that never logged in');
    assertSame('', $anonymous['login'] ?? null, 'login of the client that never logged in');

    $first->close();

    // Freeing one handle must leave the other fully usable, network included.
    $second->login(MOCK_LOGIN, MOCK_PASSWORD);
    $model = decodeObject($second->getGameModel(MOCK_GAME_ID), 'getGameModel() on the surviving client');
    assertSame(MOCK_GAME_ID, $model['GameId'] ?? null, 'GameId from the surviving client');

    $second->close();
});

$checks->run('close() is idempotent', static function () use ($newClient): void {
    $client = $newClient();
    $client->close();
    $client->close();
});

$checks->run('a closed client throws instead of crashing', static function () use ($newClient): void {
    $closed = $newClient();
    $closed->close();

    try {
        $closed->domain();
    } catch (EncxException $e) {
        assertTrue($e->getMessage() !== '', 'EncxException carries no message');

        return;
    } catch (\Throwable $e) {
        fail(sprintf('want Encx\EncxException, got %s: %s', $e::class, $e->getMessage()));
    }

    fail('domain() on a closed client returned instead of throwing');
});

$checks->run('package-level helpers work', static function (): void {
    $text = Helpers::loginErrorText(0);
    assertTrue(is_string($text), sprintf('loginErrorText() returned %s, want string', get_debug_type($text)));
    assertTrue($text !== '', 'loginErrorText(0) returned an empty string');
});

$client->close();

exit($checks->report());
