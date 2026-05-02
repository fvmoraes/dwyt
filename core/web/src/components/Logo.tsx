interface Props {
  size?: number
  showText?: boolean
}

/**
 * DWYT mascot — nerd cartoon guy (Dwight-ish energy).
 * Glasses, flat center-parted hair, serious face, tie.
 */
export default function Logo({ size = 32, showText = true }: Props) {
  const vw = showText ? 160 : 32
  const vh = 40
  const w  = showText ? Math.round((size / vh) * vw) : size
  const h  = size

  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox={`0 0 ${vw} ${vh}`}
      width={w}
      height={h}
      fill="none"
      aria-label="DWYT"
    >
      {/* ── Character (scaled) ── */}
      <g transform="translate(0,2) scale(0.72)">
        {/* Neck */}
        <rect x="13" y="23" width="6" height="4" rx="1" fill="#c8a882"/>

        {/* Shirt + tie */}
        <path d="M10 27 Q13 24 16 26 Q19 24 22 27 L24 34 H8 Z" fill="#e9ecef"/>
        <path d="M15 25 L16 24 L17 25 L16.5 31 Z" fill="#f08d49"/>
        <polygon points="15,25 17,25 16.5,26.5" fill="#e67700"/>

        {/* Head */}
        <ellipse cx="16" cy="16" rx="8" ry="9" fill="#c8a882"/>

        {/* Hair */}
        <path d="M8 13 Q8 6 16 6 Q24 6 24 13 Q22 8 16 8.5 Q10 8 8 13 Z" fill="#3d2b1f"/>
        <line x1="16" y1="6" x2="16" y2="9" stroke="#2a1a0e" strokeWidth="0.6"/>

        {/* Ears */}
        <ellipse cx="8.2"  cy="16" rx="1.5" ry="2" fill="#c8a882"/>
        <ellipse cx="23.8" cy="16" rx="1.5" ry="2" fill="#c8a882"/>

        {/* Eyes */}
        <ellipse cx="13" cy="16" rx="2.2" ry="2" fill="white"/>
        <ellipse cx="19" cy="16" rx="2.2" ry="2" fill="white"/>
        <circle cx="13.3" cy="16.2" r="1.3" fill="#3d2b1f"/>
        <circle cx="19.3" cy="16.2" r="1.3" fill="#3d2b1f"/>
        <circle cx="13.5" cy="16.3" r="0.5" fill="#1a1b1e"/>
        <circle cx="19.5" cy="16.3" r="0.5" fill="#1a1b1e"/>
        <circle cx="13.8" cy="15.7" r="0.35" fill="white"/>
        <circle cx="19.8" cy="15.7" r="0.35" fill="white"/>

        {/* Glasses */}
        <rect x="10.5" y="14" width="5" height="4" rx="1.2"
          fill="none" stroke="#1a1b1e" strokeWidth="1.1"/>
        <rect x="16.5" y="14" width="5" height="4" rx="1.2"
          fill="none" stroke="#1a1b1e" strokeWidth="1.1"/>
        <line x1="15.5" y1="16" x2="16.5" y2="16" stroke="#1a1b1e" strokeWidth="1"/>
        <line x1="10.5" y1="15.5" x2="8.5"  y2="15" stroke="#1a1b1e" strokeWidth="1"/>
        <line x1="21.5" y1="15.5" x2="23.5" y2="15" stroke="#1a1b1e" strokeWidth="1"/>

        {/* Eyebrows */}
        <line x1="11"   y1="13.5" x2="15.5" y2="13.2"
          stroke="#3d2b1f" strokeWidth="1.1" strokeLinecap="round"/>
        <line x1="16.5" y1="13.2" x2="21"   y2="13.5"
          stroke="#3d2b1f" strokeWidth="1.1" strokeLinecap="round"/>

        {/* Nose */}
        <path d="M15.5 18 Q16 19.5 16.5 18"
          stroke="#a07850" strokeWidth="0.8" fill="none" strokeLinecap="round"/>

        {/* Mouth — straight, determined */}
        <line x1="13.5" y1="21" x2="18.5" y2="21"
          stroke="#a07850" strokeWidth="1" strokeLinecap="round"/>
      </g>

      {/* ── Text ── */}
      {showText && (
        <text x="30" y="28"
          fontFamily="'SF Mono','Fira Code','Cascadia Code',monospace"
          fontSize="22" fontWeight="700" letterSpacing="1"
          fill="#c1c2c5"
        >
          DW<tspan fill="#3bc9db">YT</tspan>
        </text>
      )}
    </svg>
  )
}
