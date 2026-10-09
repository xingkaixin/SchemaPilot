const styles: Record<
  string,
  { badge: "blue" | "teal" | "purple" | "red" | "orange"; tile: string; params: string }
> = {
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
};

export function driverTone(driver: string) {
  return styles[driver]?.tile ?? "bg-kumo-tint text-kumo-subtle";
}

export function driverBadge(driver: string) {
  return styles[driver]?.badge ?? "blue";
}

export function driverParamsExample(driver: string) {
  return styles[driver]?.params ?? "";
}
