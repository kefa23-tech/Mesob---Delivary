export type Delivery = {
  order_id: string;
  status: string;
  restaurant_name: string;
  restaurant_lat: number;
  restaurant_lng: number;
  delivery_address: string;
  delivery_lat: number;
  delivery_lng: number;
  customer_name: string;
  customer_phone: string;
  total_minor: number;
  delivery_fee_minor: number;
};
