declare module 'react-simple-maps' {
  import * as React from 'react';

  export interface ComposableMapProps extends React.SVGProps<SVGSVGElement> {
    width?: number;
    height?: number;
    projection?: string | ((...args: any[]) => any);
    projectionConfig?: Record<string, any>;
    children?: React.ReactNode;
  }

  export const ComposableMap: React.FC<ComposableMapProps>;

  export interface GeographiesProps {
    geography: string | Record<string, any> | string[];
    children: (props: { geographies: any[] }) => React.ReactNode;
    parseGeographies?: (geos: any[]) => any[];
  }

  export const Geographies: React.FC<GeographiesProps>;

  export interface GeographyProps {
    geography: any;
    onMouseEnter?: (e: React.MouseEvent<SVGPathElement, MouseEvent>) => void;
    onMouseLeave?: (e: React.MouseEvent<SVGPathElement, MouseEvent>) => void;
    onClick?: (e: React.MouseEvent<SVGPathElement, MouseEvent>) => void;
    style?: {
      default?: React.CSSProperties;
      hover?: React.CSSProperties;
      pressed?: React.CSSProperties;
    };
    className?: string;
  }

  export const Geography: React.FC<GeographyProps>;

  export interface MarkerProps {
    coordinates: [number, number];
    children?: React.ReactNode;
    className?: string;
  }

  export const Marker: React.FC<MarkerProps>;

  export const ZoomableGroup: React.FC<any>;
  export const Sphere: React.FC<any>;
  export const Graticule: React.FC<any>;
}
