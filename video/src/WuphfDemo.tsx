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
      {/* Timeline:
          0-4.5       Scene 1 Cold Open
          4.5-10.5    Scene 2 Command        (6s)
          10.5-18     Scene 3 Meet Team      (7.5s)
          18-29       Scene 4 They Work      (11s)
          29-38.5     Scene 5 DM Redirect    (9.5s)
          38.5-48.5   Scene 5b System Learns (10s)
          48.5-72.5   Scene 6 Efficiency     (24s — fourth-wall break)
          72.5-79.5   Scene 7 Close          (7s)
      */}

      <Sequence from={sec(0)} durationInFrames={sec(4.5)}>
        <Scene1ColdOpen />
      </Sequence>

      <Sequence from={sec(4.5)} durationInFrames={sec(6)}>
        <Scene2TheCommand />
      </Sequence>

      <Sequence from={sec(10.5)} durationInFrames={sec(7.5)}>
        <Scene3MeetTheTeam />
      </Sequence>

      <Sequence from={sec(18)} durationInFrames={sec(11)}>
        <Scene4TheyWork />
      </Sequence>

      <Sequence from={sec(29)} durationInFrames={sec(9.5)}>
        <Scene5DmRedirect />
      </Sequence>

      <Sequence from={sec(38.5)} durationInFrames={sec(10)}>
        <Scene5bSystemLearns />
      </Sequence>

      <Sequence from={sec(48.5)} durationInFrames={sec(24)}>
        <Scene6MoneyShot />
      </Sequence>

      <Sequence from={sec(72.5)} durationInFrames={sec(13.5)}>
        <Scene7TheClose />
      </Sequence>

      {/* ─── NARRATION ─── */}

      <Sequence from={sec(5)} durationInFrames={sec(5.5)}>
        <Audio src={staticFile("audio/narration-scene2.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(11)} durationInFrames={sec(6.5)}>
        <Audio src={staticFile("audio/narration-scene3.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(18)} durationInFrames={sec(11)}>
        <Audio src={staticFile("audio/narration-scene4-new.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(29)} durationInFrames={sec(10)}>
        <Audio src={staticFile("audio/narration-scene5.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(38.8)} durationInFrames={sec(10)}>
        <Audio src={staticFile("audio/narration-scene5b.mp3")} volume={0.95} />
      </Sequence>

      {/* Scene 6 — three clips with a fourth-wall break in the middle */}
      <Sequence from={sec(49.5)} durationInFrames={sec(7)}>
        <Audio src={staticFile("audio/narration-scene6a.mp3")} volume={0.95} />
      </Sequence>
      <Sequence from={sec(55.8)} durationInFrames={sec(12.5)}>
        <Audio src={staticFile("audio/narration-scene6b-cut.mp3")} volume={0.95} />
      </Sequence>
      <Sequence from={sec(67.8)} durationInFrames={sec(5)}>
        <Audio src={staticFile("audio/narration-scene6c.mp3")} volume={0.95} />
      </Sequence>

      <Sequence from={sec(72.8)} durationInFrames={sec(13.5)}>
        <Audio src={staticFile("audio/narration-scene7-tight.mp3")} volume={0.95} />
      </Sequence>

      {/* ─── iOS TEXT-MESSAGE DINGS on message arrivals ─── */}
      {/* Scene 4 starts at 18s */}
      <Sequence from={sec(18) + 15} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.3} />
      </Sequence>
      <Sequence from={sec(18) + 55} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.25} />
      </Sequence>
      <Sequence from={sec(18) + 110} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.2} />
      </Sequence>
      <Sequence from={sec(18) + 150} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.2} />
      </Sequence>
      {/* Scene 5 starts at 29s */}
      <Sequence from={sec(29) + 15} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.25} />
      </Sequence>
      <Sequence from={sec(29) + 65} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.25} />
      </Sequence>
      <Sequence from={sec(29) + 115} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.2} />
      </Sequence>

      {/* Transitions */}
      <Sequence from={sec(10.5) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.86} />
      </Sequence>
      <Sequence from={sec(18) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.67} />
      </Sequence>
      <Sequence from={sec(48.5) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.86} />
      </Sequence>

      {/* Record scratch at music cut */}
      <Sequence from={sec(55.8)} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/record-scratch.mp3")} volume={0.45} />
      </Sequence>

      {/* Background music — funk/upbeat, cuts at break, resumes with play */}
      <Sequence from={sec(0)} durationInFrames={sec(55.8)}>
        <Audio src={staticFile("audio/bg-music-d.mp3")} volume={0.13} loop />
      </Sequence>
      <Sequence from={sec(67.8)} durationInFrames={sec(18.2)}>
        <Audio src={staticFile("audio/bg-music-d.mp3")} volume={0.13} loop />
      </Sequence>
    </AbsoluteFill>
  );
};
