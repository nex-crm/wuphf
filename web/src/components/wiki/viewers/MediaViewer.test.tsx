import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import MediaViewer from "./MediaViewer";

// Mock the tokened-URL helper so the test exercises only the viewer's own
// extension routing + load/error handling, not the auth plumbing.
vi.mock("../../../api/wiki", () => ({
  wikiFileUrl: vi.fn(
    (path: string) => `https://broker.test/wiki/file?path=${path}`,
  ),
}));

describe("<MediaViewer>", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders a video element with the tokened src for video extensions", () => {
    render(<MediaViewer path="team/assets/clip.mp4" />);
    const video = screen.getByLabelText(/video: clip\.mp4/i);
    expect(video.tagName).toBe("VIDEO");
    expect(video).toHaveAttribute(
      "src",
      "https://broker.test/wiki/file?path=team/assets/clip.mp4",
    );
    expect(screen.getByText("Loading video…")).toBeInTheDocument();
    fireEvent.loadedData(video);
    expect(video).not.toHaveAttribute("hidden");
    expect(screen.queryByText("Loading video…")).not.toBeInTheDocument();
  });

  it("renders an audio element for audio extensions and reveals it on canplay", () => {
    render(<MediaViewer path="team/assets/voice.mp3" />);
    const audio = screen.getByLabelText(/audio: voice\.mp3/i);
    expect(audio.tagName).toBe("AUDIO");
    expect(audio).toHaveAttribute("hidden");
    fireEvent.canPlay(audio);
    expect(audio).not.toHaveAttribute("hidden");
  });

  it("renders the error state when the media element errors", () => {
    render(<MediaViewer path="team/assets/broken.webm" />);
    const video = screen.getByLabelText(/video: broken\.webm/i);
    fireEvent.error(video);
    expect(screen.getByRole("alert")).toHaveTextContent(
      /could not play video file “broken\.webm”/i,
    );
  });

  it("exposes Download + open-in-new-tab actions for a playable file", () => {
    render(<MediaViewer path="team/assets/clip.mp4" />);
    const download = screen.getByRole("link", { name: /download/i });
    expect(download).toHaveAttribute("download", "clip.mp4");
    const openTab = screen.getByRole("link", { name: /open in new tab/i });
    expect(openTab).toHaveAttribute("target", "_blank");
  });

  it("renders an audio file inside a labelled now-playing card", () => {
    render(<MediaViewer path="team/assets/voice.mp3" />);
    const audio = screen.getByLabelText(/audio: voice\.mp3/i);
    // The audio control lives in a card alongside a filename caption.
    expect(audio.closest(".wk-viewer__audio-card")).not.toBeNull();
  });

  it("renders an empty state for unsupported extensions", () => {
    render(<MediaViewer path="team/assets/notes.txt" />);
    expect(
      screen.getByText(/“notes\.txt” is not a playable media file/i),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText(/video:|audio:/i)).not.toBeInTheDocument();
  });
});
