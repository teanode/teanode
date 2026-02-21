import React from "react";
import { Outlet } from "@tanstack/react-router";

/** /conversations — layout route that renders child routes via Outlet. */
export default function ConversationsLayout() {
  return <Outlet />;
}
