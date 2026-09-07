import { useLang } from '../LangContext'

export default function LangToggle() {
  const { lang, toggle } = useLang()
  return (
    <button
      onClick={toggle}
      title={lang === 'en' ? 'Switch to Portuguese' : 'Mudar para Inglês'}
      style={{
        background: 'transparent',
        border: '1px solid var(--border)',
        borderRadius: '6px',
        padding: '2px 6px',
        fontSize: '15px',
        cursor: 'pointer',
        display: 'flex',
        alignItems: 'center',
        gap: '4px',
        color: 'var(--muted)',
        lineHeight: 1,
      }}
    >
      {lang === 'en' ? (
        // Show PT-BR flag to switch to Portuguese
        <>🇧🇷 <span style={{ fontSize: '12px' }}>PT</span></>
      ) : (
        // Show US flag to switch to English
        <>🇺🇸 <span style={{ fontSize: '12px' }}>EN</span></>
      )}
    </button>
  )
}
