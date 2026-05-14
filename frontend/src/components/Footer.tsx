import { Heart } from "lucide-react";

// Lucide ships only generic icons; brand marks live elsewhere. Inline a
// small GitHub glyph rather than pulling in a separate brand-icon dep.
function GitHubIcon({ size = 12 }: { size?: number }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8a8 8 0 0 0 5.47 7.59c.4.07.55-.17.55-.38v-1.33c-2.23.48-2.7-1.07-2.7-1.07-.36-.92-.89-1.17-.89-1.17-.73-.5.05-.49.05-.49.81.06 1.24.83 1.24.83.72 1.23 1.88.88 2.34.67.07-.52.28-.88.51-1.08-1.78-.2-3.65-.89-3.65-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.13 0 0 .67-.21 2.2.82a7.7 7.7 0 0 1 4 0c1.53-1.04 2.2-.82 2.2-.82.44 1.11.16 1.93.08 2.13.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.74.54 1.48v2.2c0 .21.15.46.55.38A8 8 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

export function Footer() {
  return (
    <footer className="footer">
      <div className="footer-row">
        <span>
          Free &amp; open source <strong>forever</strong>, just like{" "}
          <a href="https://nats.io" target="_blank" rel="noreferrer noopener">
            NATS
          </a>
          .
        </span>
        <span className="footer-sep">·</span>
        <span>
          Built with <Heart size={12} className="footer-heart" aria-label="love" /> by{" "}
          <a
            href="https://github.com/1995parham"
            target="_blank"
            rel="noreferrer noopener"
            className="footer-link"
          >
            <GitHubIcon /> 1995parham
          </a>
        </span>
        <span className="footer-sep">·</span>
        <a
          href="https://github.com/1995parham/cheshmhayash"
          target="_blank"
          rel="noreferrer noopener"
          className="footer-link"
        >
          <GitHubIcon /> 1995parham/cheshmhayash
        </a>
      </div>
    </footer>
  );
}
