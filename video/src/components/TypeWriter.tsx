import { useCurrentFrame } from "remotion";
import { fonts, colors } from "../theme";

interface TypeWriterProps {
  text: string;
  startFrame?: number;
  charsPerFrame?: number;
  style?: React.CSSProperties;
  cursorColor?: string;
}

export const TypeWriter: React.FC<TypeWriterProps> = ({
  text,
  startFrame = 0,
  charsPerFrame = 0.8,
  style,
  cursorColor = colors.green,
}) => {
  const frame = useCurrentFrame();
  const elapsed = Math.max(0, frame - startFrame);
  const visibleChars = Math.min(text.length, Math.floor(elapsed * charsPerFrame));
  const showCursor = elapsed > 0;
  const cursorBlink = Math.floor(frame / 15) % 2 === 0;

  return (
    <span
      style={{
        fontFamily: fonts.mono,
        whiteSpace: "pre",
        ...style,
      }}
    >
      {text.slice(0, visibleChars)}
      {showCursor && (
        <span
          style={{
            opacity: cursorBlink ? 1 : 0,
            color: cursorColor,
            fontWeight: "bold",
          }}
        >
          _
        </span>
      )}
    </span>
  );
};
