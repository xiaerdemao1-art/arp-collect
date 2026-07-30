import React from 'react';

interface SunflowerIconProps {
  size?: number;
  style?: React.CSSProperties;
}

// 梵高向日葵风格图标
const SunflowerIcon: React.FC<SunflowerIconProps> = ({ size = 80, style }) => {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 100 100"
      style={style}
      xmlns="http://www.w3.org/2000/svg"
    >
      {/* 背景光晕 */}
      <defs>
        <radialGradient id="sunflowerGlow" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="#fbbf24" stopOpacity="0.4" />
          <stop offset="100%" stopColor="#fbbf24" stopOpacity="0" />
        </radialGradient>
        <filter id="paintStroke">
          <feTurbulence type="fractalNoise" baseFrequency="0.04" numOctaves="2" />
          <feDisplacementMap in="SourceGraphic" scale="2" />
        </filter>
      </defs>
      
      {/* 光晕 */}
      <circle cx="50" cy="50" r="48" fill="url(#sunflowerGlow)" />
      
      {/* 花瓣 - 外层 */}
      {[...Array(12)].map((_, i) => (
        <ellipse
          key={`outer-${i}`}
          cx="50"
          cy="20"
          rx="8"
          ry="18"
          fill={i % 2 === 0 ? '#f59e0b' : '#fbbf24'}
          stroke="#d97706"
          strokeWidth="1"
          transform={`rotate(${i * 30} 50 50)`}
          style={{ filter: 'url(#paintStroke)' }}
        />
      ))}
      
      {/* 花瓣 - 内层 */}
      {[...Array(12)].map((_, i) => (
        <ellipse
          key={`inner-${i}`}
          cx="50"
          cy="28"
          rx="6"
          ry="12"
          fill={i % 2 === 0 ? '#fcd34d' : '#fde68a'}
          stroke="#f59e0b"
          strokeWidth="0.5"
          transform={`rotate(${i * 30 + 15} 50 50)`}
        />
      ))}
      
      {/* 花心 - 外圈 */}
      <circle
        cx="50"
        cy="50"
        r="18"
        fill="#92400e"
        stroke="#78350f"
        strokeWidth="2"
      />
      
      {/* 花心 - 内圈 */}
      <circle
        cx="50"
        cy="50"
        r="12"
        fill="#78350f"
      />
      
      {/* 花心纹理 - 种子点 */}
      {[...Array(20)].map((_, i) => {
        const angle = (i * 137.5 * Math.PI) / 180; // 黄金角
        const r = Math.sqrt(i) * 2.5;
        const x = 50 + r * Math.cos(angle);
        const y = 50 + r * Math.sin(angle);
        return (
          <circle
            key={`seed-${i}`}
            cx={x}
            cy={y}
            r="1.5"
            fill={i % 3 === 0 ? '#a16207' : '#ca8a04'}
          />
        );
      })}
      
      {/* 高光 */}
      <ellipse
        cx="45"
        cy="45"
        rx="4"
        ry="3"
        fill="rgba(255,255,255,0.3)"
        transform="rotate(-30 45 45)"
      />
    </svg>
  );
};

export default SunflowerIcon;
