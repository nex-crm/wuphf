import { AbsoluteFill, Audio, Sequence, staticFile } from "remotion";
import { sec } from "./theme";
import { Scene1ColdOpen } from "./scenes/Scene1ColdOpen";
import { Scene2TheCommand } from "./scenes/Scene2TheCommand";
import { Scene3PickAPack } from "./scenes/v27/Scene3PickAPack";
import { Scene4TheyWork } from "./scenes/Scene4TheyWork";
import { Scene5DmRedirect } from "./scenes/Scene5DmRedirect";
import { Scene5bSystemLearns } from "./scenes/Scene5bSystemLearns";
import { Scene5cWikiAndNotebooks } from "./scenes/Scene5cWikiAndNotebooks";
import { Scene6MoneyShot } from "./scenes/Scene6MoneyShot";
import { Scene7TheClose } from "./scenes/Scene7TheClose";

// 2026-04-22 cut — v26 narrations byte-for-byte, v26 scene cadence,
// plus one new wiki+notebooks beat (Scene 5c) with its own narration
// recorded in voice jCwvXwuIgwNySUf5STNy. Scene 3 uses the approved
// Pick-a-Pack redesign. Everything downstream of 5c shifts +11s.

export const WuphfDemo: React.FC = () => {
  return (
    <AbsoluteFill style={{ backgroundColor: "#000" }}>
      {/* Timeline:
          0-4.5        Scene 1 Cold Open
          4.5-10.5     Scene 2 Command        (6s)
          10.5-18      Scene 3 Pick a Pack    (7.5s)
          18-29        Scene 4 They Work      (11s)
          29-38.5      Scene 5 DM Redirect    (9.5s)
          38.5-48.5    Scene 5b System Learns (10s)
          48.5-59.5    Scene 5c Wiki + Notebooks (11s)  ← NEW
          59.5-83.5    Scene 6 Money Shot     (24s)
          83.5-97      Scene 7 Close          (13.5s)
      */}

      <Sequence from={sec(0)} durationInFrames={sec(4.5)}>
        <Scene1ColdOpen />
      </Sequence>

      <Sequence from={sec(4.5)} durationInFrames={sec(6)}>
        <Scene2TheCommand />
      </Sequence>

      <Sequence from={sec(10.5)} durationInFrames={sec(7.5)}>
        <Scene3PickAPack />
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

      <Sequence from={sec(48.5)} durationInFrames={sec(11)}>
        <Scene5cWikiAndNotebooks />
      </Sequence>

      <Sequence from={sec(59.5)} durationInFrames={sec(24)}>
        <Scene6MoneyShot />
      </Sequence>

      <Sequence from={sec(83.5)} durationInFrames={sec(13.5)}>
        <Scene7TheClose />
      </Sequence>

      {/* ─── NARRATION — original v26 clips, byte-for-byte ─── */}

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

      {/* NEW wiki + notebooks narration — voice jCwvXwuIgwNySUf5STNy, deadpan */}
      <Sequence from={sec(48.9)} durationInFrames={sec(10.5)}>
        <Audio src={staticFile("audio/narration-wiki-notebooks.mp3")} volume={0.95} />
      </Sequence>

      {/* Scene 6 narrations — shifted +11s to make room for 5c */}
      <Sequence from={sec(60.5)} durationInFrames={sec(7)}>
        <Audio src={staticFile("audio/narration-scene6a.mp3")} volume={0.95} />
      </Sequence>
      <Sequence from={sec(66.8)} durationInFrames={sec(12.5)}>
        <Audio src={staticFile("audio/narration-scene6b-cut.mp3")} volume={0.95} />
      </Sequence>
      <Sequence from={sec(78.8)} durationInFrames={sec(5)}>
        <Audio src={staticFile("audio/narration-scene6c.mp3")} volume={0.95} />
      </Sequence>

      {/* Scene 7 narration — shifted +11s */}
      <Sequence from={sec(83.8)} durationInFrames={sec(13.5)}>
        <Audio src={staticFile("audio/narration-scene7-tight.mp3")} volume={0.95} />
      </Sequence>

      {/* ─── iOS TEXT-MESSAGE DINGS on message arrivals (Scene 4 + 5) ─── */}
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
      <Sequence from={sec(29) + 15} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.25} />
      </Sequence>
      <Sequence from={sec(29) + 65} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.25} />
      </Sequence>
      <Sequence from={sec(29) + 115} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.2} />
      </Sequence>

      {/* Soft tick on the wiki→notebooks tab-switch beat */}
      <Sequence from={sec(48.5) + sec(3.2)} durationInFrames={sec(0.6)}>
        <Audio src={staticFile("audio/pristine.mp3")} volume={0.18} />
      </Sequence>

      {/* Transitions — whooshes 3 frames before scene cuts */}
      <Sequence from={sec(10.5) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.86} />
      </Sequence>
      <Sequence from={sec(18) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.67} />
      </Sequence>
      <Sequence from={sec(48.5) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.86} />
      </Sequence>
      {/* Whoosh into Scene 5c + into Scene 6 */}
      <Sequence from={sec(48.5) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.6} />
      </Sequence>
      <Sequence from={sec(59.5) - 3} durationInFrames={sec(1)}>
        <Audio src={staticFile("audio/whoosh.mp3")} volume={0.7} />
      </Sequence>

      {/* Record scratch at the Scene 6 music cut — shifts +11s */}
      <Sequence from={sec(66.8)} durationInFrames={sec(1.5)}>
        <Audio src={staticFile("audio/record-scratch.mp3")} volume={0.45} />
      </Sequence>

      {/* Background music — same cut-and-resume pattern, shifted to match
          the new Scene 6 start. First segment covers 0 → record scratch,
          second picks up after the fourth-wall bit. */}
      <Sequence from={sec(0)} durationInFrames={sec(66.8)}>
        <Audio src={staticFile("audio/bg-music-d.mp3")} volume={0.13} loop />
      </Sequence>
      <Sequence from={sec(78.8)} durationInFrames={sec(18.2)}>
        <Audio src={staticFile("audio/bg-music-d.mp3")} volume={0.13} loop />
      </Sequence>
    </AbsoluteFill>
  );
};
