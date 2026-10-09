export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg viewBox="0 0 64 64" width={size} height={size} aria-hidden="true">
      <rect x="8" y="44" width="22" height="10" rx="5" fill="currentColor" opacity="0.35" />
      <rect x="8" y="30" width="32" height="10" rx="5" fill="currentColor" opacity="0.7" />
      <path d="M13 16h33l10 5-10 5H13a5 5 0 0 1 0-10z" fill="#E8590C" />
    </svg>
  );
}
