export function driverTone(driver: string) {
  if (driver === "postgres") return "bg-kumo-badge-blue/12 text-kumo-link";
  if (driver === "mysql") return "bg-kumo-badge-teal/12 text-kumo-badge-teal";
  return "bg-kumo-tint text-kumo-subtle";
}
