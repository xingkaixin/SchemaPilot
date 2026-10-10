import type { DriverInfo } from "../api";

const paramsExamples: Record<string, string> = {
  postgres: "sslmode=disable",
  mysql: "tls=skip-verify&charset=utf8mb4",
  sqlserver: "encrypt=disable",
  oracle: "SSL=enable&SSL VERIFY=false",
  sqlite: "_pragma=journal_mode(WAL)",
  opengauss: "sslmode=disable",
  dm: "compatibleMode=oracle",
  xugu: "CHAR_SET=GBK",
};

const databaseLabels: Record<string, string> = { oracle: "服务名", dm: "模式（schema）" };

export function driverDatabaseLabel(driver?: DriverInfo) {
  return databaseLabels[driver?.id ?? ""] ?? "数据库";
}

// Compatible databases fall back to the example of the protocol they speak.
export function driverParamsExample(driver?: DriverInfo) {
  if (!driver) return "";
  return paramsExamples[driver.id] ?? paramsExamples[driver.protocol ?? ""] ?? "";
}
