import React from "react";
import { PixelAvatar } from "./PixelAvatar";

interface AgentAvatarProps {
  name: string;
  color: string;
  size?: number;
  status?: "active" | "idle";
}

export const AgentAvatar: React.FC<AgentAvatarProps> = ({
  name,
  color,
  size = 36,
  status = "active",
}) => {
  const slug = name.toLowerCase().replace(/ /g, "").slice(0, 3);

  return (
    <div style={{ position: "relative", width: size, height: size, flexShrink: 0 }}>
      <PixelAvatar slug={slug} color={color} size={size} />
      {/* Status dot */}
      <div
        style={{
          position: "absolute",
          bottom: -2,
          right: -2,
          width: size * 0.3,
          height: size * 0.3,
          borderRadius: "50%",
          backgroundColor: status === "active" ? "#2BAC76" : "#868686",
          border: "2px solid #1A1D21",
        }}
      />
    </div>
  );
};
