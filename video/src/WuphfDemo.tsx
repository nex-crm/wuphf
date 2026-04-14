import { AbsoluteFill, Audio, Sequence, staticFile } from "remotion";
import { sec } from "./theme";
import { Scene1ColdOpen } from "./scenes/Scene1ColdOpen";
import { Scene2TheCommand } from "./scenes/Scene2TheCommand";
import { Scene3MeetTheTeam } from "./scenes/Scene3MeetTheTeam";
import { Scene4TheyWork } from "./scenes/Scene4TheyWork";
import { Scene5DmRedirect } from "./scenes/Scene5DmRedirect";
import { Scene5bSystemLearns } from "./scenes/Scene5bSystemLearns";
import { Scene6MoneyShot } from "./scenes/Scene6MoneyShot";
import { Scene7TheClose } from "./scenes/Scene7TheClose";

export const WuphfDemo: React.FC = () => {
  return (
    <AbsoluteFill style={{ backgroundColor: "#000" }}>
      {/* ─── VISUALS ─── */}
      {/* Timeline:
          0-4.5       Scene 1 Cold Open
          4.5-11.5    Scene 2 Command        (7s, narr 5.4s)
          11.5-20     Scene 3 Meet Team      (8.5s, narr 6s)
          20-32.5     Scene 4 They Work      (12.5s, narr 11.8s)
          32.5-42     Scene 5 DM Redirect    (9.5s, narr 9.2s)
          42-54       Scene 5b System Learns (12s, narr 10.9s)
          54-66.5     Scene 6 Efficiency     (12.5s, narr 9.2s)
          66.5-74     Scene 7 Close          (7.5s, narr 6.1s)
      */}

      <Sequence from={sec(0)} durationInFrames={sec(4.5)}>
        <Scene1ColdOpen />
      </Sequence>

      <Sequence from={sec(4.5)} durationInFrames={sec(7)}>
        <Scene2TheCommand />
      </Sequence>

      <Sequence from={sec(11.5)} durationInFrames={sec(8.5)}>
        <Scene3MeetTheTeam />
      </Sequence>

      <Sequence from={sec(20)} durationInFrames={sec(12.5)}>
        <Scene4TheyWork />
      </Sequence>

      <Sequence from={sec(32.5)} durationInFrames={sec(9.5)}>
        <Scene5DmRedirect />
      </Sequence>

      <Sequence from={sec(42)} durationInFrames={sec(12)}>
        <Scene5bSystemLearns />
      </Sequence>

      <Sequence from={sec(54)} durationInFrames={sec(12.5)}>
        <Scene6MoneyShot />
      </Sequence>

      <Sequence from={sec(66.5)} durationInFrames={sec(7.5)}>
        <Scene7TheClose />
      </Sequence>

      {/* ─── NARRATION ─── */}

      <Sequence from={sec(5)} durationInFrames={sec(6.5)}>
        <Audio src={staticFile("audio/narration-scene2.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(12.5)} durationInFrames={sec(7)}>
        <Audio src={staticFile("audio/narration-scene3.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(20.5)} durationInFrames={sec(12)}>
        <Audio src={staticFile("audio/narration-scene4.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(33)} durationInFrames={sec(9.5)}>
        <Audio src={staticFile("audio/narration-scene5.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(42.5)} durationInFrames={sec(11.5)}>
        <Audio src={staticFile("audio/narration-scene5b.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(55)} durationInFrames={sec(11)}>
        <Audio src={staticFile("audio/narration-scene6.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(67)} durationInFrames={sec(7)}>
        <Audio src={staticFile("audio/narration-scene7.mp3")} volume={0.95} />
      </Sequence>

      {/* ─── iPhone DING on message arrivals (Scene 4 starts at 20s) ─── */}

      <Sequence from={sec(20) + 15} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/notif-tritone.mp3")} volume={0.3} />
      </Sequence>
      <Sequence from={sec(20) + 55} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/notif-tritone.mp3")} volume={0.25} />
      </Sequence>
      <Sequence from={sec(20) + 110} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/notif-tritone.mp3")} volume={0.2} />
      </Sequence>
      <Sequence from={sec(20) + 150} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/notif-tritone.mp3")} volume={0.2} />
      </Sequence>

      <Sequence from={sec(32.5) + 15} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/notif-tritone.mp3")} volume={0.25} />
      </Sequence>
      <Sequence from={sec(32.5) + 65} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/notif-tritone.mp3")} volume={0.25} />
      </Sequence>
      <Sequence from={sec(32.5) + 115} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/notif-tritone.mp3")} volume={0.2} />
      </Sequence>

      {/* Transitions */}
      <Sequence from={sec(11.5) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.2} />
      </Sequence>
      <Sequence from={sec(20) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.15} />
      </Sequence>
      <Sequence from={sec(54) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.2} />
      </Sequence>

      {/* ─── BACKGROUND MUSIC ─── */}
      <Sequence from={sec(0)} durationInFrames={sec(74)}>
        <Audio src={staticFile("audio/bg-music.mp3")} volume={0.04} loop />
      </Sequence>
    </AbsoluteFill>
  );
};
