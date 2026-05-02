import { createContext, useContext, useState, type ReactNode } from 'react'
import type { Lang, Strings } from './i18n'
import { T } from './i18n'

interface LangCtx { lang: Lang; t: Strings; toggle: () => void }

const Ctx = createContext<LangCtx>({
  lang: 'en',
  t: T.en,
  toggle: () => {},
})

export function LangProvider({ children }: { children: ReactNode }) {
  const stored = (localStorage.getItem('dwyt-lang') as Lang) || 'en'
  const [lang, setLang] = useState<Lang>(stored)

  function toggle() {
    const next: Lang = lang === 'en' ? 'pt' : 'en'
    setLang(next)
    localStorage.setItem('dwyt-lang', next)
  }

  return <Ctx.Provider value={{ lang, t: T[lang], toggle }}>{children}</Ctx.Provider>
}

export function useLang() { return useContext(Ctx) }
