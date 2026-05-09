function sendAlive(): void {
  if (typeof process.send === "function") {
    process.send({ alive: true });
  }
}

setInterval(sendAlive, 1_000);
