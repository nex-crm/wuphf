import React from "react";
import { fonts } from "../theme";

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
  const initials = name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();

  return (
    <div style={{ position: "relative", width: size, height: size, flexShrink: 0 }}>
      <div
        style={{
          width: size,
          height: size,
          borderRadius: 8,
          backgroundColor: color,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          fontFamily: fonts.sans,
          fontSize: size * 0.38,
          fontWeight: 700,
          color: "#FFF",
          letterSpacing: -0.5,
        }}
      >
        {initials}
      </div>
      {/* Status dot */}
      <div
        style={{
          position: "absolute",
          bottom: -2,
          right: -2,
          width: size * 0.3,
          height: size * 0.3,
          borderRadius: "50%",
          backgroundColor: status === "active" ? "#44B078" : "#868686",
          border: "2px solid #1A1D21",
        }}
      />
    </div>
  );
};
