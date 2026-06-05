import { addons } from 'storybook/manager-api'
import { create } from 'storybook/theming'

// Brand the Storybook manager (sidebar header) with the tela lockup. Dark base
// to match the dev-tool register; brandImage is the logo-lockup-dark.svg inlined
// as a data URI (white wordmark reads on the dark manager chrome).
const brandImage =
  'data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAzNjggOTYiIHdpZHRoPSIzNjgiIGhlaWdodD0iOTYiIHJvbGU9ImltZyIgYXJpYS1sYWJlbD0idGVsYSI+CiAgPGcgdHJhbnNmb3JtPSJ0cmFuc2xhdGUoMTIsMTIpIj48c3ZnIHdpZHRoPSI3MiIgaGVpZ2h0PSI3MiIgdmlld0JveD0iMCAwIDUxMiA1MTIiPjxyZWN0IHdpZHRoPSI1MTIiIGhlaWdodD0iNTEyIiByeD0iMTEyIiBmaWxsPSIjNGY0NmU1Ii8+PGcgdHJhbnNmb3JtPSJ0cmFuc2xhdGUoLTIuNSwtMzguNSkiPjxwYXRoIGZpbGw9IiNmZmZmZmYiIGQ9Ik0xNTAgMjQwIEwxOTYgMTg4IFEyMDUgMTc4IDIxOCAxNzggSDM1NiBRMzc4IDE3OCAzNjYgMjAwIEwzMzIgMjQwIFEzMjUgMjUwIDMxMiAyNTAgSDE2MiBRMTQwIDI1MCAxNTAgMjQwIFoiLz48cGF0aCBmaWxsPSIjZmZmZmZmIiBkPSJNMjM4IDI1MCBIMjk2IFEzMDAgMjUwIDMwMCAyNjggVjM5NiBRMzAwIDQxNCAyODEgNDEwIEwyNDUgNDAyIFEyMjYgMzk4IDIyNiAzODAgVjI3MiBRMjI2IDI1MiAyMzggMjUwIFoiLz48cGF0aCBmaWxsPSIjMWUxYjRiIiBmaWxsLW9wYWNpdHk9IjAuNCIgZD0iTTI1MCAyNTAgSDMxMiBRMzI1IDI1MCAzMzIgMjQwIEwzMDQgMjcwIFEyOTcgMjc4IDI5NyAyOTAgVjMyNCBaIi8+PC9nPjwvc3ZnPjwvZz4KICA8dGV4dCB4PSIxMDAiIHk9IjY0IiBmb250LWZhbWlseT0iR2Vpc3QsIEludGVyLCAtYXBwbGUtc3lzdGVtLCAnU2Vnb2UgVUknLCBzeXN0ZW0tdWksIHNhbnMtc2VyaWYiIGZvbnQtc2l6ZT0iNTYiIGZvbnQtd2VpZ2h0PSI2ODAiIGxldHRlci1zcGFjaW5nPSItMyIgZmlsbD0iI2Y4ZmFmYyI+dGVsYTwvdGV4dD4KPC9zdmc+Cg=='

addons.setConfig({
  theme: create({
    base: 'dark',
    brandTitle: 'tela — components',
    brandUrl: 'https://tela.cagdas.io',
    brandImage,
    brandTarget: '_self',
  }),
})
