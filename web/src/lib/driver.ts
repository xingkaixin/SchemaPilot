import type { DriverInfo } from "../api";

type Style = {
  badge: "blue" | "teal" | "purple" | "red" | "orange" | "green" | "neutral";
  tile: string;
  params: string;
};

const styles: Record<string, Style> = {
  postgres: {
    badge: "blue",
    tile: "bg-kumo-badge-blue/12 text-kumo-link",
    params: "sslmode=disable",
  },
  mysql: {
    badge: "teal",
    tile: "bg-kumo-badge-teal/12 text-kumo-badge-teal",
    params: "tls=skip-verify&charset=utf8mb4",
  },
  sqlserver: {
    badge: "purple",
    tile: "bg-kumo-badge-purple/12 text-kumo-badge-purple",
    params: "encrypt=disable",
  },
  oracle: {
    badge: "red",
    tile: "bg-kumo-badge-red/12 text-kumo-badge-red",
    params: "SSL=enable&SSL VERIFY=false",
  },
  sqlite: {
    badge: "orange",
    tile: "bg-kumo-badge-orange/12 text-kumo-badge-orange",
    params: "_pragma=journal_mode(WAL)",
  },
  opengauss: {
    badge: "green",
    tile: "bg-kumo-badge-green/12 text-kumo-badge-green",
    params: "sslmode=disable",
  },
  dm: { badge: "neutral", tile: "bg-kumo-fill text-kumo-subtle", params: "compatibleMode=oracle" },
  xugu: { badge: "neutral", tile: "bg-kumo-fill text-kumo-subtle", params: "CHAR_SET=GBK" },
};

const databaseLabels: Record<string, string> = { oracle: "服务名", dm: "模式（schema）" };

export function driverDatabaseLabel(driver?: DriverInfo) {
  return databaseLabels[driver?.id ?? ""] ?? "数据库";
}

// Compatible databases take the look of the protocol they speak.
function styleOf(driver?: DriverInfo): Style | undefined {
  if (!driver) return undefined;
  return styles[driver.id] ?? styles[driver.protocol ?? ""];
}

export function driverTone(driver?: DriverInfo) {
  return styleOf(driver)?.tile ?? "bg-kumo-tint text-kumo-subtle";
}

export function driverBadge(driver?: DriverInfo) {
  return styleOf(driver)?.badge ?? "blue";
}

export function driverParamsExample(driver?: DriverInfo) {
  return styleOf(driver)?.params ?? "";
}
