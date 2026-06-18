import { backendProxy } from "$lib/proxy";

const proxy = backendProxy();

export const GET = proxy;
export const POST = proxy;
