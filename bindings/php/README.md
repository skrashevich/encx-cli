# PHP-биндинги encx

PHP-обёртка над клиентом Encounter из `encx`. Go-пакет `mobile/encxmobile` собирается
в разделяемую библиотеку (`-buildmode=c-shared`), а PHP вызывает её через FFI. Клиент
живёт на стороне Go, поэтому сессия, куки и HAR-запись сохраняются между вызовами —
PHP держит только целочисленный хендл.

Биндинги **генерируются** из Go-исходника, поэтому не могут от него отстать: см.
[Синхронизация](#синхронизация).

## Требования

| Что | Зачем |
| --- | --- |
| PHP 8.1+ с расширением `ffi` | загрузка библиотеки и вызовы |
| Go (версия из `go.mod`) | сборка библиотеки и генерация |
| Компилятор C (clang/gcc) и `CGO_ENABLED=1` | cgo-сборка `c-shared` |

Composer не нужен: рядом лежит `autoload.php`. `composer.json` есть для тех, кто
подключает биндинги как пакет.

Проверить, что FFI доступен:

```sh
php -r 'var_dump(extension_loaded("ffi"));'
```

## Сборка

```sh
bash bindings/php/build.sh
```

Скрипт кладёт `libencx.dylib` (macOS), `libencx.so` (Linux) или `encx.dll` (Windows)
в `bindings/php/lib/`. Каталог не версионируется — библиотеку собирают локально или в CI.

## Использование

```php
<?php

require __DIR__ . '/bindings/php/autoload.php';

$client = Encx\Client::newClient('demo.en.cx', false);

$login = json_decode($client->login('user', 'password'), true);
if ($login['Error'] !== 0) {
    throw new RuntimeException(Encx\Helpers::loginErrorText($login['Error']));
}

$model = json_decode($client->getGameModel(82448), true);
echo $model['GameTitle'], "\n";

$client->close();
```

Для домена без TLS (например локальный `encx-mock`) используйте расширенную фабрику:

```php
$client = Encx\Client::newClientWithOptions('127.0.0.1:18080', false, true, 10, 'ru');
```

Аргументы: домен, `insecureTLS`, `useHTTP`, таймаут в секундах, язык.

### Как читать сигнатуры

Имена методов — это Go-имена в lowerCamelCase: `GetGameModel` → `getGameModel`,
`APIBaseURL` → `apiBaseURL`, `ExportHAR` → `exportHAR`. Отображение типов:

| Go | PHP |
| --- | --- |
| `string` | `string` |
| `int64` | `int` |
| `bool` | `bool` |
| `[]byte` | `string` (бинарная строка; base64 разбирается за вас) |
| `(T, error)` | `T`, а ошибка бросается как `Encx\EncxException` |
| `error` | `void`, ошибка бросается |
| `*HARSnapshot` | `array` |

Методы, возвращающие JSON (`login`, `getGameModel`, `getProfile`, …), отдают строку —
разбирайте её `json_decode`, как это делают Swift/Kotlin с теми же биндингами.

### Время жизни клиента

Каждый `Client` владеет одним хендлом Go-клиента. Вызывайте `close()`, когда закончили;
деструктор делает то же для клиента, переживившего последнее использование. Повторный
`close()` безвреден, а обращение к закрытому клиенту бросает `EncxException`.

## Что связано, а что нет

Связано 49 символов: 2 фабрики, методы `*EncClient` (игра, коды, команды, профиль,
куки, HAR) и 5 статических функций в `Encx\Helpers`. Полный машиночитаемый список —
в `bindings.manifest.json`.

Не связано 20 символов, и манифест хранит причину для каждого:

- **`IsAntiSpamError`, `AntiSpamURLFromError`, `IsUndecodableAcceptedError`** — принимают
  `error`. Go-ошибка это интерфейсное значение, у него нет представления в C ABI.
  Из PHP та же информация доступна через текст сообщения `EncxException`.
- **`EncClient.NewAgentSession`, `StartCodexDeviceLogin`** и методы `AgentSession` и
  `CodexDeviceLogin` — это отдельные объекты с внутренним состоянием (мьютексы,
  провайдеры, делегаты). Реестр хендлов сейчас держит только `*EncClient`, поэтому
  агентская часть API остаётся вне биндингов.

Список не поддерживается вручную: он вычисляется из исходника при каждой генерации.

## Синхронизация

Из Go-исходника генерируются пять файлов:

| Файл | Что это |
| --- | --- |
| `cshared/exports_gen.go` | cgo-обёртки с `//export` |
| `encx.h` | C-заголовок, который читает `FFI::cdef` |
| `bindings.manifest.json` | снимок поверхности: связанное и пропущенное с причинами |
| `src/Encx/Client.php` | класс клиента |
| `src/Encx/Helpers.php` | пакетные функции |

Правки в них не переживут следующую генерацию — каждый помечен `DO NOT EDIT`.
Рукописны только `src/Encx/Ffi.php`, `src/Encx/EncxException.php`, `cshared/runtime.go`
и `autoload.php`: они реализуют механику вызова, а не поверхность API.

Регенерация:

```sh
go generate ./bindings/...
```

Отставание биндингов от кода ловят четыре независимых механизма:

1. **Кодогенерация.** Поверхность не пишется руками, поэтому расходиться нечему:
   `bindings/php/cmd/encxphpgen` разбирает AST `mobile/encxmobile` и печатает все
   пять файлов.
2. **Go-тест.** `go test ./bindings/...` перегенерирует артефакты в память и падает,
   называя устаревший файл и первую разошедшуюся строку.
3. **CI.** `.github/workflows/bindings.yml` запускает генерацию и падает при непустом
   `git diff` по `bindings/`, затем собирает библиотеку и гоняет e2e.
4. **Pre-commit hook.** Устанавливается один раз:

   ```sh
   bash bindings/php/hooks/install.sh
   ```

   Хук срабатывает только на коммитах, задевающих `mobile/encxmobile/` или
   `bindings/php/`, перегенерирует биндинги и блокирует коммит с устаревшими файлами.
   Индекс он не правит: что попадёт в коммит — решение автора, а не хука.

Быстрая проверка актуальности без записи файлов:

```sh
cd bindings/php && go run ./cmd/encxphpgen -out . -check
```

## Тесты

```sh
go test ./bindings/... -count=1        # генератор, runtime, drift-детектор
bash bindings/php/tests/run-e2e.sh     # сквозной прогон PHP → FFI → Go → encx-mock
```

E2E-раннер самодостаточен: он собирает библиотеку, поднимает `encx-mock` на свободном
порту, прогоняет `tests/e2e.php` и гасит сервер. Тест проверяет в том числе, что
сессия переживает вызовы, что ошибка Go приходит исключением, а не `null`, и что
`[]byte` ходит через C ABI в обе стороны.

## Добавление другого языка

`bindings/` рассчитан на несколько языков. Модель поверхности
(`bindings/php/internal/surface`) не зависит от PHP: она описывает пакет
`mobile/encxmobile` в терминах типов, переносимых через C ABI. Новый язык — это новый
эмиттер поверх той же модели плюс свой раздел в манифесте.
