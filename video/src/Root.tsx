import { Composition } from "remotion";
import { WuphfDemo } from "./WuphfDemo";

// 30fps, 50 seconds = 1500 frames
const FPS = 30;
const DURATION = 74;

export const Root: React.FC = () => {
  return (
    <>
      <Composition
        id="WuphfDemo"
        component={WuphfDemo}
        durationInFrames={FPS * DURATION}
        fps={FPS}
        width={1920}
        height={1080}
      />
    </>
  );
};
