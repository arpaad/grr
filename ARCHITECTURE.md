# `grr` – Go Registry Resolver
> Verziószám: 0.1 | Státusz: implementálva

> Ez a fájl a v0.1 tervezési dokumentuma és a megvalósítás közben hozott döntések napló­ja — a "miért van úgy, ahogy van" forrása. A jövőbeli munkát a [plan.md](plan.md) (2. fázis roadmap) vezeti, ez itt a történeti referencia.

---

## Alapkoncepció

Névhez kötött factory/value tároló. Általános célú primitív – nem HTTP-specifikus, nem DI framework. Önállóan használható, a `gold` erre épül.

---

## Repo struktúra

```
github.com/arpaad/grr       ← core + net/http middleware
github.com/arpaad/grr-gin   ← Gin middleware, külön repo, dep: grr
github.com/arpaad/gold      ← Logic réteg, külön repo, dep: grr
```

Minden repo független release ciklus. Ha később jön Echo, Fiber vagy más – új repo, nem érinti a többit.

---

## Fájlszerkezet

```
grr/
├── registry.go       # Registry struct, Default, New, NewFrom
├── register.go       # Set, Register, RegisterFunc, RegisterScoped
├── resolve.go        # Resolve, Get
├── scope.go          # BeginScope, cleanupScope
├── introspect.go     # IsRegistered, Clear
├── middleware/
│   └── http.go       # net/http middleware – stdlib, Chi, Echo
└── registry_test.go
```

---

## Publikus API

### Létrehozás

```go
grr.Default               // globális singleton – sugar, belépési pont
r := grr.New()            // izolált registry, nincs parent
r := grr.NewFrom(parent)  // parent chain – fallback lookup
```

### Regisztráció

```go
// Singleton sugar – factory mindig ugyanazt adja vissza
r.Set("key", value)

// Transient vagy singleton – a factory függvény dönti el
r.Register("key", func(ctx context.Context) any {
    return newSomething(ctx)
})

// Sugar – ctx nélküli factory
r.RegisterFunc("key", func() any {
    return newSomething()
})

// Scoped – scope ID alapján egy példány per scope
r.RegisterScoped("key", func(ctx context.Context) any {
    return newSomething(ctx)
})
```

> **Nincs return érték** – duplikált regisztráció panic, nem error.

### Lekérdezés

```go
r.Resolve(ctx, "key")  // univerzális – ctx mindig van
r.Get("key")           // sugar = Resolve(context.Background(), "key")
```

### Scope lifecycle

```go
ctx, endScope := r.BeginScope(ctx)
defer endScope()
// endScope: sync.Once védett – kétszeri hívás nem panic
// ctx.Done() esetén goroutine automatikusan cleanup-ol
```

### Introspekció

```go
r.IsRegistered("key")  // bool – kondicionális regisztrációhoz
r.Clear()              // teljes reset – főleg test teardown
```

### Context helpers

```go
grr.WithRegistry(ctx, r)   // registry csatolása ctx-re
grr.RegistryFromCtx(ctx)   // registry kiolvasása ctx-ből, fallback: grr.Default
```

---

## Lifetime szemantika

| Regisztráció | Lifetime | Ki dönti el |
|---|---|---|
| `Set` | singleton | sugar – factory mindig ugyanazt adja |
| `Register` | transient vagy singleton | a factory függvény logikája |
| `RegisterFunc` | transient vagy singleton | a factory függvény logikája |
| `RegisterScoped` | scoped | registry – scope ID alapján |

**Context nélküli scoped = transient** – nincs scope azonosítás, ezért minden resolve új példányt ad.

---

## Belső struktúra

```go
type entry struct {
    factory func(ctx context.Context) any
    scoped  bool
}

type Registry struct {
    mu      sync.RWMutex
    entries map[string]*entry
    parent  *Registry

    scopeMu sync.RWMutex
    scopes  map[uint64]map[string]any  // scopeID → key → instance
}
```

**RWMutex stratégia:**
- `Resolve` / `Get` → `RLock` – párhuzamos olvasók nem blokkolják egymást
- `Register` / `Set` / `Clear` → `Lock` – kizárólagos írás
- `scopes` map külön `scopeMu`-val védett

---

## Viselkedési garanciák

| Eset | Viselkedés |
|---|---|
| Duplikált regisztráció ugyanabban a registry-ben | **panic** |
| Scoped resolve scope ID nélkül | **panic** |
| `Get` scoped entry-re | **panic** |
| Key nem található, nincs parent | **panic** |
| Key nem található, van parent | parent chain lookup |
| Párhuzamos resolve | RWMutex – biztonságos |
| `endScope` kétszer hívva | sync.Once – nem panic |
| `ctx.Done()` endScope előtt | goroutine cleanup – nem leak |

---

## Scope belső működése

```go
func (r *Registry) BeginScope(parent context.Context) (context.Context, func()) {
    scopeID := atomic.AddUint64(&scopeCounter, 1)
    ctx := context.WithValue(parent, scopeKey{}, scopeID)

    stop := make(chan struct{})

    go func() {
        select {
        case <-ctx.Done():
        case <-stop:
        }
        r.cleanupScope(scopeID)
    }()

    var once sync.Once
    endScope := func() {
        once.Do(func() { close(stop) })
    }

    return ctx, endScope
}
```

---

## HTTP Middleware

### net/http (stdlib, Chi, Echo) – core részben

```go
// grr/middleware/http.go
func Middleware(r *Registry) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
            ctx := WithRegistry(req.Context(), r)
            ctx, endScope := r.BeginScope(ctx)
            defer endScope()
            next.ServeHTTP(w, req.WithContext(ctx))
        })
    }
}
```

### Gin – külön repo (`github.com/arpaad/grr-gin`)

```go
func Middleware(r *grr.Registry) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx := grr.WithRegistry(c.Request.Context(), r)
        ctx, endScope := r.BeginScope(ctx)
        defer endScope()
        c.Request = c.Request.WithContext(ctx)
        c.Next()
    }
}
```

---

## Parent chain – fallback lookup

```
grr.Default          ← gyökér, mindig jelen van
    └── appRegistry  ← NewFrom(Default), production override
            └── r    ← NewFrom(appRegistry), test override
```

---

## Tesztelési pattern

```go
// Csak egy elemet override-olok, a többi fallback a Default-ra
func TestSomething(t *testing.T) {
    r := grr.NewFrom(grr.Default)
    r.Register("db", func(ctx context.Context) any {
        return &mockDB{}
    })
    svc := NewUserService(r)
}

// Teljesen izolált – nincs fallback
func TestIsolated(t *testing.T) {
    r := grr.New()
    r.Set("config", testConfig)
    r.Register("db", mockDBFactory)
}
```

---

## Ami nem tartozik a `grr`-be

| Funkció | Hol lesz |
|---|---|
| Típusbiztonság – cast a hívó dolga, `any` alapú | `gold` réteg |
| Domain fogalom – Logic, port/adapter szeparáció | `gold` |
| Scoped kikényszerítése compile time-ban | `gold` |
| Pooled lifetime | később, külön tervezés |
| Observability hook | később, külön tervezés |

---

## Nyílt kérdések

- [x] Modul path véglegesítve: `github.com/arpaad/grr`
- [x] `grr` betűszó véglegesítve: **Go Registry Resolver** – a két alapművelet (Register, Resolve) adja a nevet
- [ ] Pooled lifetime tervezése (max N példány, release mechanizmus) — **2. fázis**
- [ ] Observability hook – mikor és hol — **2. fázis**

---

## Implementáció közben felmerült döntések / talált hibák

> v0.1 implementáció (2026-06-16) – core + middleware + tesztek megírva, minden zöld.

- **Ellentmondás feloldva:** a "Lifetime szemantika" szekció szerint *"Context nélküli scoped = transient"*, a "Viselkedési garanciák" tábla szerint *"Scoped resolve scope ID nélkül → panic"*. A két állítás ütközött. A `gold` terv hibafilozófiája ("Do – scoped, nincs BeginScope → panic") alapján a **panic** verziót implementáltam – ez a konzisztens viselkedés mindkét repóban. A "Context nélküli scoped = transient" mondat a táblázat fölött **elavult**, érdemes törölni.
- **Scope-tárolás globális, nem per-Registry:** mivel a scope ID már globálisan egyedi (atomic counter), a scope cache-t nem az adott `Registry` struct-on tartom (`r.scopes`), hanem egy package-szintű táblában. Ok: ha egy scoped entry a **parent** registry-ben van regisztrálva, de a `BeginScope`-ot egy **child** registry-n hívják meg, a cache-nek mindenképp elérhetőnek kell lennie – nem a regisztráló, hanem a scope-ot kezdő oldalon. Per-Registry tárolással ez hibásan panic-olt volna.
- **Talált és javított deadlock:** az első implementációban a teljes scope cache-t egyetlen mutex védte, amit a `Resolve` a factory hívása *alatt* is fogva tartott. Ha egy scoped factory maga is `Resolve`-olt egy másik scoped kulcsot ugyanabban a scope-ban (gyakori eset: egy repo modell függ egy másik scoped erőforrástól, pl. egy tranzakciótól), **deadlock** lépett fel, mert a mutex nem reentrant. Megoldás: minden (scope, kulcs) párhoz saját `sync.Once`, a factory hívása nem tart zárva semmilyen megosztott lockot. Lefedve teszttel (`TestScopedFactoryCanResolveAnotherScopedKey`, `TestScopedConcurrentResolveBuildsOnce`).
- **Körkörös függőség – megoldva, panic:** a `Resolve` minden hívási láncban végigviszi a `ctx`-en, mely kulcsok építése van éppen folyamatban (`buildchain.go`). Ha egy factory (scoped vagy nem-scoped) közvetlenül vagy közvetve önmagát próbálja resolve-olni, ez **panic**-ot dob a teljes lánccal (`grr: circular dependency: a -> b -> a`), *mielőtt* a korábbi implementáció deadlockolt volna (scoped esetben a nem-reentrant `sync.Once.Do` miatt) vagy stack overflow-zott volna (nem-scoped, transient esetben). A legitim, nem köríves nested resolve (egy modell egy másik scoped erőforrástól függ) továbbra is hibátlanul működik – ezt külön lánc-bejegyzés különbözteti meg a körtől. Lásd `TestCircularScopedDependencyPanics`, `TestCircularDependencyThreeKeysPanics`, `TestCircularNonScopedDependencyPanics`.
- **`endScope` szinkron takarítás:** a tervben vázolt `close(stop)` mintát kiegészítettem azzal, hogy `endScope` *azonnal*, szinkron módon töröl a scope cache-ből (nem csak a háttér-goroutine-on keresztül, aszinkron). Indok: a hívó `defer endScope()` után determinisztikusan elvárja, hogy a scope véget érjen – aszinkron takarítással ez race lett volna (a teszt is elkapta).