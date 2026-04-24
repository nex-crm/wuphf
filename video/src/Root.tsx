import { Composition } from "remotion";
import { WuphfDemo } from "./WuphfDemo";
import { WuphfWikiScroll } from "./WuphfWikiScroll";
import { WuphfChannel } from "./WuphfChannel";
import { WuphfSkills } from "./WuphfSkills";
import { WuphfTasks } from "./WuphfTasks";

const FPS = 30;
const DURATION = 97;

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
      <Composition
        id="WuphfWikiScroll"
        component={WuphfWikiScroll}
        durationInFrames={180}
        fps={FPS}
        width={1270}
        height={760}
      />
      <Composition
        id="WuphfChannel"
        component={WuphfChannel}
        durationInFrames={180}
        fps={FPS}
        width={1270}
        height={760}
      />
      <Composition
        id="WuphfSkills"
        component={WuphfSkills}
        durationInFrames={30}
        fps={FPS}
        width={1270}
        height={760}
      />
      <Composition
        id="WuphfTasks"
        component={WuphfTasks}
        durationInFrames={30}
        fps={FPS}
        width={1270}
        height={760}
      />
    </>
  );
};
