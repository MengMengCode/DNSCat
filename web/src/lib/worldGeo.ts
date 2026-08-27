import { geoNaturalEarth1, geoPath } from 'd3-geo';
import * as topojson from 'topojson-client';
import worldTopoData from '../data/countries-110m.json';

/**
 * 世界地图几何数据层：把 Natural Earth 110m TopoJSON 投影为 SVG 路径。
 * 使用 Natural Earth 投影（平面世界地图），并把结果裁剪为紧凑的 viewBox，
 * 供组件以纯 SVG 渲染，不依赖 canvas 图表库。
 */

export interface WorldCountryPath {
  /** 稳定的 React key */
  key: string;
  /** Natural Earth 中的国家 / 地区名称，用于与 API 国家码映射对齐 */
  name: string;
  /** SVG path 的 d 属性 */
  d: string;
  /** 投影后的质心，用于键盘聚焦时定位提示框 */
  centroid: [number, number];
}

export interface WorldMapGeometry {
  countries: WorldCountryPath[];
  width: number;
  height: number;
}

const VIEW_WIDTH = 960;

let cachedGeometry: WorldMapGeometry | null = null;

const buildGeometry = (): WorldMapGeometry => {
  const topology = worldTopoData as any;
  const collection = topojson.feature(topology, topology.objects.countries) as any;

  const features: any[] = Array.isArray(collection?.features) ? collection.features : [];

  const projection = geoNaturalEarth1();
  projection.fitWidth(VIEW_WIDTH, collection);

  // 将投影结果左上角对齐到原点，得到没有多余留白的 viewBox。
  const measured = geoPath(projection);
  const [[minX, minY], [maxX, maxY]] = measured.bounds(collection);
  const [translateX, translateY] = projection.translate();
  projection.translate([translateX - minX, translateY - minY]);

  const path = geoPath(projection);
  const countries: WorldCountryPath[] = [];

  features.forEach((feature, index) => {
    const d = path(feature);
    if (!d) return;

    const centroid = path.centroid(feature);
    countries.push({
      key: String(feature?.id ?? index),
      name: String(feature?.properties?.name ?? ''),
      d,
      centroid: [centroid[0], centroid[1]],
    });
  });

  return {
    countries,
    width: Math.ceil(maxX - minX),
    height: Math.ceil(maxY - minY),
  };
};

export const getWorldMapGeometry = (): WorldMapGeometry => {
  if (!cachedGeometry) {
    cachedGeometry = buildGeometry();
  }
  return cachedGeometry;
};
