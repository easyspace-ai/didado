import * as fs from "fs";
import * as path from "path";

export class FileLogger {
  private static logDir = path.join(process.cwd(), "logs");

  private static ensureDir() {
    if (!fs.existsSync(this.logDir)) {
      fs.mkdirSync(this.logDir, { recursive: true });
    }
  }

  private static getFilePath() {
    const date = new Date().toISOString().split("T")[0]; // YYYY-MM-DD
    return path.join(this.logDir, `bot-${date}.log`);
  }

  static log(level: "INFO" | "WARN" | "ERROR" | "TRADE", message: string) {
    this.ensureDir();
    const filePath = this.getFilePath();
    const timestamp = new Date().toISOString();
    // Strip ANSI color codes for clean text logs
    const cleanMessage = message.replace(/\u001b\[\d+m/g, "");
    const line = `[${timestamp}] [${level}] ${cleanMessage}\n`;

    fs.appendFile(filePath, line, (err) => {
      if (err) console.error("❌ Failed to write to log file:", err);
    });
  }
}
