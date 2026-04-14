import { Composition } from "remotion";
import { WuphfDemo } from "./WuphfDemo";

const FPS = 30;
const DURATION = 86;

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
