# Changelog

Format : [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/), versions [SemVer](https://semver.org/lang/fr/).
Les changements de comportement figurent sous **Action requise**.

## [v1.4.1] — 2026-11-01

Code identique à la v1.4.0, qui est rétractée (`retract`) parce qu'elle a été publiée avec un fichier interne.

### Action requise
- **Derrière un reverse proxy (HAProxy, nginx)** : déclarer le proxy avec `WithTrustedProxies("<ip/cidr>")`.
  Sans cela, `c.ClientIP()` renvoie l'IP du proxy pour tous les clients : rate-limits partagés, logs faussés.
- Passer à Go ≥ 1.27.3 (CVE de la bibliothèque standard dans `net/http`, `http2`, `crypto/tls`, `crypto/x509`).

### Changé
- `X-Forwarded-For` n'est plus cru par défaut (aucun proxy de confiance).
- Un `X-Request-ID` entrant invalide (> 128 caractères, ou hors de `[A-Za-z0-9._-]`) est remplacé par un UUID v4.
- Une méthode non supportée sur un chemin connu renvoie `405` avec l'en-tête `Allow` au lieu de `404`.
- `WithMaxBodySize` répond `413` dès que `Content-Length` dépasse la limite, avant le handler.
- `/ready` renvoie `503` dès que `Stop()` commence.
- Les valeurs sensibles de la query (`code`, `state`, `token`, `password`…) sont masquées dans les logs slog.

### Ajouté
- `WithTrustedProxies`, `WithHSTS`, `MicroService.Errors()`, `GinService.SetShuttingDown()`, `SecureHeadersWithHSTS`.
- En-têtes `Content-Security-Policy: frame-ancestors 'none'`, `Cross-Origin-Opener-Policy`, `Cross-Origin-Resource-Policy` sur Public.
- `HEAD` et `Cache-Control: no-store` sur `/health`, `/ready`, `/version`.
- `WaitForShutdown()` se termine aussi quand un serveur ne parvient pas à démarrer.

## [v1.4.0] — rétractée

## [v1.3.0]

Options `WithReadinessCheck`, `WithVersion`, `WithSecureHeaders`, `WithMaxBodySize`, `With*Routes`, `With*Port` ; routes techniques intégrées.
