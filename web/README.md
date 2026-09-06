# tutor web

Phone-first PWA for practising CBSE Class 11 Python.

```
npm install
npm run dev        # http://localhost:5173, proxies /v1 -> http://localhost:8080
npm test           # vitest unit tests
npm run typecheck  # tsc --noEmit
npm run build      # typecheck + production build into dist/
```

`npm run preview` serves `dist/` locally; the service worker only registers on a production build.
