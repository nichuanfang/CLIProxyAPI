const path = require("path");

const isWindows = process.platform === "win32";
const binaryName = isWindows ? "cli-proxy-api.exe" : "cli-proxy-api";

module.exports = {
  apps: [
    {
      name: "cli-proxy-api",
      script: path.join(__dirname, binaryName),
      args: ["--config", path.join(__dirname, "config.yaml")],
      cwd: __dirname,
      autorestart: true,
      max_restarts: 10,
      restart_delay: 3000,
      watch: false
    }
  ]
};