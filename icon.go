package main

// A swarm: many peers, one file coming together. Drawn rather than shipped as
// a binary asset so the install stays a single self-contained executable.
const iconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" width="128" height="128">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#3ddc97"/>
      <stop offset="1" stop-color="#19a974"/>
    </linearGradient>
  </defs>
  <rect x="4" y="4" width="120" height="120" rx="26" fill="#10231c"/>
  <g stroke="url(#g)" stroke-width="3" opacity="0.75" fill="none">
    <path d="M64 64 L32 34"/><path d="M64 64 L96 34"/><path d="M64 64 L26 74"/>
    <path d="M64 64 L102 74"/><path d="M64 64 L46 100"/><path d="M64 64 L84 100"/>
  </g>
  <g fill="url(#g)">
    <circle cx="32" cy="34" r="7"/><circle cx="96" cy="34" r="7"/>
    <circle cx="26" cy="74" r="6"/><circle cx="102" cy="74" r="6"/>
    <circle cx="46" cy="100" r="6"/><circle cx="84" cy="100" r="6"/>
  </g>
  <circle cx="64" cy="64" r="16" fill="#eafff6"/>
  <path d="M64 55 L64 71 M57 65 l7 7 7-7" stroke="#10231c" stroke-width="4"
        stroke-linecap="round" stroke-linejoin="round" fill="none"/>
</svg>
`
